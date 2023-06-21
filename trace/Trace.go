//github.com/whatap/go-api/trace
package trace

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	whatapboot "github.com/whatap/go-api/agent/agent/boot"
	agentconfig "github.com/whatap/go-api/agent/agent/config"
	agenttrace "github.com/whatap/go-api/agent/agent/trace"
	agentapi "github.com/whatap/go-api/agent/agent/trace/api"

	"github.com/whatap/golib/io"
	"github.com/whatap/golib/util/dateutil"
	"github.com/whatap/golib/util/hash"
	"github.com/whatap/golib/util/hexa32"
	"github.com/whatap/golib/util/iputil"
	"github.com/whatap/golib/util/keygen"
	"github.com/whatap/golib/util/stringutil"
	"github.com/whatap/golib/util/urlutil"
)

const (
	PACKET_DB_MAX_SIZE           = 4 * 1024  // max size of sql
	PACKET_SQL_MAX_SIZE          = 32 * 1024 // max size of sql
	PACKET_HTTPC_MAX_SIZE        = 32 * 1024 // max size of sql
	PACKET_MESSAGE_MAX_SIZE      = 32 * 1024 // max size of message
	PACKET_METHOD_STACK_MAX_SIZE = 32 * 1024 // max size of message

	COMPILE_FILE_MAX_SIZE = 2 * 1024 // max size of filename

	HTTP_HOST_MAX_SIZE   = 2 * 1024 // max size of host
	HTTP_URI_MAX_SIZE    = 2 * 1024 // max size of uri
	HTTP_METHOD_MAX_SIZE = 256      // max size of method
	HTTP_IP_MAX_SIZE     = 256      // max size of ip(request_addr)
	HTTP_UA_MAX_SIZE     = 2 * 1024 // max size of user agent
	HTTP_REF_MAX_SIZE    = 2 * 1024 // max size of referer
	HTTP_USERID_MAX_SIZE = 2 * 1024 // max size of userid

	HTTP_PARAM_MAX_COUNT      = 20
	HTTP_PARAM_KEY_MAX_SIZE   = 255 // = 을 빼고 255 byte
	HTTP_PARAM_VALUE_MAX_SIZE = 256

	HTTP_HEADER_MAX_COUNT      = 20
	HTTP_HEADER_KEY_MAX_SIZE   = 255 // = 을 빼고 255 byte
	HTTP_HEADER_VALUE_MAX_SIZE = 256

	SQL_PARAM_MAX_COUNT      = 20
	SQL_PARAM_VALUE_MAX_SIZE = 256

	STEP_ERROR_MESSAGE_MAX_SIZE = 4 * 1024
)

var (
	WHATAP_COOKIE_NAME = "WHATAP"
	traceLock          sync.Mutex
)

type WrapResponseWriter struct {
	http.ResponseWriter
	Status int
}

func (l *WrapResponseWriter) WriteHeader(status int) {
	l.Status = status
	l.ResponseWriter.WriteHeader(status)
}

func Init(m map[string]string) {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	if m != nil {
		agentconfig.SetValues(&m)
	}
	// embeded
	go whatapboot.Boot()
}

func Shutdown() {
	// whatapnet.UdpShutdown()
}

func GetTraceContext(ctx context.Context) (context.Context, *TraceCtx) {
	if ctx == nil {
		return ctx, nil
	}
	if v := ctx.Value("whatap"); v != nil {
		return ctx, v.(*TraceCtx)
	}

	// TO-DO goroutine id
	// if v := GetGIDTraceCtx(GetGID()); v != nil {
	// 	return ctx, v
	// }

	return ctx, nil
}

func GetAgentTraceContext(tCtx *TraceCtx) *agenttrace.TraceContext {
	if tCtx != nil {
		return tCtx.Ctx
	}
	return nil
}

func NewTraceContext(ctx context.Context) (context.Context, *TraceCtx) {
	if ctx == nil {
		ctx = context.Background()
	}
	var traceCtx *TraceCtx
	traceCtx = PoolTraceContext()
	traceCtx.GID = GetGID()
	traceCtx.Ctx = agenttrace.PoolTraceContext()

	wCtx := traceCtx.Ctx
	wCtx.Txid = keygen.Next()
	traceCtx.Txid = wCtx.Txid

	ctx = context.WithValue(ctx, "whatap", traceCtx)
	// TO-DO goroutine id
	// AddGIDTraceCtx(traceCtx.GID, traceCtx)
	return ctx, traceCtx
}

func Start(ctx context.Context, name string) (context.Context, error) {
	conf := agentconfig.GetConfig()
	if !conf.Enabled {
		return ctx, nil
	}

	ctx, traceCtx := NewTraceContext(ctx)
	traceCtx.Name = name
	traceCtx.StartTime = dateutil.SystemNow()
	// update multi trace info
	UpdateMtrace(traceCtx, http.Header{})

	wCtx := traceCtx.Ctx
	wCtx.StartTime = traceCtx.StartTime
	wCtx.ServiceURL = urlutil.NewURL(name)
	agentapi.StartTx(wCtx)

	return ctx, nil
}

func StartWithRequest(r *http.Request) (context.Context, error) {
	conf := agentconfig.GetConfig()
	if !conf.Enabled {
		return r.Context(), nil
	}

	ctx, traceCtx := NewTraceContext(r.Context())
	traceCtx.Name = r.RequestURI
	traceCtx.StartTime = dateutil.SystemNow()
	// update multi trace info
	UpdateMtrace(traceCtx, r.Header)

	wCtx := traceCtx.Ctx
	wCtx.StartTime = traceCtx.StartTime
	wCtx.ServiceURL = urlutil.NewURL(filepath.Join(r.Host, "/", r.RequestURI))
	ipaddr := r.RemoteAddr
	if strings.Index(ipaddr, ",") > -1 {
		ipArray := strings.Split(ipaddr, ",")
		if len(ipArray) > 1 {
			ipaddr = ipArray[0]
		}
	}
	wCtx.RemoteIp = io.ToInt(iputil.ToBytes(ipaddr), 0)
	wCtx.HttpMethod = r.Method
	wCtx.RefererURL = urlutil.NewURL(r.Referer())
	wCtx.UserAgentString = r.UserAgent()
	wCtx.WClientId = int64(hash.HashStr(GetClientId(r)))
	if conf.Debug {
		log.Println("[WA-TX-02001] StartWithRequest: ", traceCtx.Txid, ", ", traceCtx.Name)
	}
	agentapi.StartTx(wCtx)

	//http.Header -> map[string][]string
	SetHeader(ctx, r.Header)

	return ctx, nil
}

func StartWithContext(ctx context.Context, name string) (context.Context, error) {
	conf := agentconfig.GetConfig()
	if !conf.Enabled {
		return ctx, nil
	}
	if ctx, traceCtx := GetTraceContext(ctx); traceCtx != nil {
		traceCtx.Name = name
		traceCtx.StartTime = dateutil.SystemNow()
		// update multi trace info
		UpdateMtrace(traceCtx, http.Header{})

		wCtx := traceCtx.Ctx
		if wCtx == nil {
			wCtx = agenttrace.PoolTraceContext()
			traceCtx.Ctx = wCtx
			wCtx.Txid = traceCtx.Txid
		}

		wCtx.StartTime = traceCtx.StartTime
		wCtx.ServiceURL = urlutil.NewURL(name)
		if conf.Debug {
			log.Println("[WA-TX-03001] StartWithContext: ", traceCtx.Txid, ", ", traceCtx.Name)
		}
		agentapi.StartTx(wCtx)
	} else {
		if conf.Debug {
			log.Println("[WA-TX-03002] StartWithContext: Not found trace context ", name)
		}
		return ctx, fmt.Errorf("Not found trace context ")
	}
	return ctx, nil
}

func SetHeader(ctx context.Context, m map[string][]string) {
	conf := agentconfig.GetConfig()
	if !conf.ProfileHttpHeaderEnabled {
		return
	}
	if _, traceCtx := GetTraceContext(ctx); traceCtx != nil {
		// http.Header -> map[string][]string
		if strings.HasPrefix(traceCtx.Name, conf.ProfileHttpHeaderUrlPrefix) {
			parsedHeader := ParseHeader(m)
			agentapi.ProfileMsg(traceCtx.Ctx, "HTTP_HEADERS", parsedHeader, 0, 0)
			if conf.Debug {
				log.Println("[WA-TX-06001] txid:", traceCtx.Txid, ", uri: ", traceCtx.Name, "\n headers: ", parsedHeader)
			}
		}
	}
}
func SetParameter(ctx context.Context, m map[string][]string) {
	conf := agentconfig.GetConfig()
	if !conf.ProfileHttpParameterEnabled {
		return
	}
	if m == nil && len(m) <= 0 {
		return
	}
	if _, traceCtx := GetTraceContext(ctx); traceCtx != nil {
		if strings.HasPrefix(traceCtx.Name, conf.ProfileHttpParameterUrlPrefix) {
			parsedParam := ParseParameter(m)
			agentapi.ProfileSecureMsg(traceCtx.Ctx, "HTTP-PARAMS", parsedParam, 0, 0)
			if conf.Debug {
				log.Println("[WA-TX-07001] HTTP-PARAMS txid:", traceCtx.Txid, ", uri: ", traceCtx.Name, "\n params: ", parsedParam)
			}
		}
	}
}
func GetClientId(r *http.Request) string {
	conf := agentconfig.GetConfig()
	if !conf.Enabled || !conf.TraceUserEnabled {
		return r.RemoteAddr
	}
	if conf.TraceUserUsingIp {
		return r.RemoteAddr
	}
	if conf.TraceUserHeaderTicketEnabled {
		for k, v := range r.Header {
			if strings.ToLower(strings.TrimSpace(k)) == strings.ToLower(strings.TrimSpace(conf.TraceUserHeaderTicket)) && len(v) > 0 {
				return v[0]
			}
		}
	}

	for _, cookie := range r.Cookies() {
		for _, v := range conf.TraceUserCookieKeys {
			if strings.ToLower(strings.TrimSpace(cookie.Name)) == strings.ToLower(strings.TrimSpace(v)) {
				return cookie.Value
			}
		}
	}

	// WhaTap Cookie name is constant WHATAP_COOKIE_NAME(WHATAP)
	for _, cookie := range r.Cookies() {
		if strings.ToUpper(strings.TrimSpace(cookie.Name)) == WHATAP_COOKIE_NAME {
			return cookie.Value
		}
	}

	return r.RemoteAddr
}
func GetWhatapCookie(r *http.Request) (cookie *http.Cookie, exists bool) {
	for _, c := range r.Cookies() {
		if c.Name == WHATAP_COOKIE_NAME {
			return c, true
		}
	}
	if cookie == nil {
		cookie = &http.Cookie{
			Name:  WHATAP_COOKIE_NAME,
			Value: fmt.Sprintf("%d", keygen.Next()),
		}
	}
	return cookie, false
}

func SetWhatapCookie(w http.ResponseWriter, cookie *http.Cookie) {
	if w != nil && cookie != nil {
		w.Header().Add("Set-Cookie", cookie.String())
	}
}

func Step(ctx context.Context, title, message string, elapsed, value int) error {
	conf := agentconfig.GetConfig()
	if !conf.Enabled {
		return nil
	}
	if _, traceCtx := GetTraceContext(ctx); traceCtx != nil {
		agentapi.ProfileMsg(traceCtx.Ctx, title, message, int32(elapsed), int32(value))
		return nil
	}

	return fmt.Errorf("Not found Txid ")
}

func Error(ctx context.Context, err error) error {
	conf := agentconfig.GetConfig()
	if !conf.Enabled {
		return nil
	}
	// udpClient := whatapnet.GetUdpClient()
	if err != nil {
		var txid int64
		var serviceName string

		if _, traceCtx := GetTraceContext(ctx); traceCtx != nil {
			agentapi.ProfileError(traceCtx.Ctx, err)
			txid = traceCtx.Txid
			serviceName = traceCtx.Name
		} else {
			agentapi.ProfileError(nil, err)
		}

		if conf.Debug {
			log.Println("[WA-TX-04001] txid:", txid, ", uri: ", serviceName, "\n error: ", err)
		}
	}
	return nil
}

func End(ctx context.Context, err error) error {
	conf := agentconfig.GetConfig()
	if !conf.Enabled {
		return nil
	}
	Error(ctx, err)
	if _, traceCtx := GetTraceContext(ctx); traceCtx != nil {
		wCtx := traceCtx.Ctx
		wCtx.Mtid = traceCtx.MTid
		wCtx.Mdepth = traceCtx.MDepth
		wCtx.McallerTxid = traceCtx.MCallerTxid
		wCtx.McallerPoidKey = traceCtx.MCallerPoidKey
		wCtx.McallerSpec = traceCtx.MCallerSpec
		wCtx.McallerUrl = traceCtx.MCallerUrl
		wCtx.Status = traceCtx.Status
		if conf.Debug {
			log.Println("[WA-TX-05001] txid: ", traceCtx.Txid, ", uri: ", traceCtx.Name,
				"\n time: ", (dateutil.SystemNow() - traceCtx.StartTime), "ms ", "\n error: ", err)
		}
		agentapi.EndTx(wCtx)
		// TO-DO goroutine id
		//RemoveGIDTraceCtx(traceCtx.GID)
		CloseTraceContext(traceCtx)
		return nil
	}
	if conf.Debug {
		log.Println("[WA-TX-05002] End: Not found Txid ", "\n error: ", err)
	}
	return fmt.Errorf("Not found Txid ")
}

func UpdateMtraceWithContext(ctx context.Context, header http.Header) {
	if _, traceCtx := GetTraceContext(ctx); traceCtx != nil {
		UpdateMtrace(traceCtx, header)
	}
}
func GetMTrace(ctx context.Context) http.Header {
	rt := make(http.Header)
	conf := agentconfig.GetConfig()
	if _, traceCtx := GetTraceContext(ctx); traceCtx != nil {
		rt.Set(conf.TraceMtraceCallerKey, traceCtx.TraceMtraceCallerValue)
		rt.Set(conf.TraceMtracePoidKey, traceCtx.TraceMtracePoidValue)
		rt.Set(conf.TraceMtraceSpecKey1, traceCtx.TraceMtraceSpecValue)

		// Mcallee
		if conf.MtraceCalleeTxidEnabled {
			traceCtx.TraceMtraceMcallee = keygen.Next()
			rt.Set(conf.TraceMtraceCalleeKey, fmt.Sprintf("%d", traceCtx.TraceMtraceMcallee))
		}
	}

	return rt
}
func UpdateMtrace(traceCtx *TraceCtx, header http.Header) {
	conf := agentconfig.GetConfig()
	if !conf.MtraceEnabled {
		return
	}
	for k, val := range header {
		if len(val) > 0 {
			v := strings.TrimSpace(val[0])
			switch strings.ToLower(strings.TrimSpace(k)) {
			case conf.TraceMtraceCallerKey:
				arr := stringutil.Split(v, ",")
				if len(arr) >= 3 {
					traceCtx.MTid = hexa32.ToLong32(arr[0])

					if val, err := strconv.Atoi(arr[1]); err == nil {
						traceCtx.MDepth = int32(val)
					}
					traceCtx.MCallerTxid = hexa32.ToLong32(arr[2])
				}
			case conf.TraceMtraceCalleeKey:
				traceCtx.MCallee = hexa32.ToLong32(v)
				if traceCtx.MCallee != 0 {
					traceCtx.Txid = traceCtx.MCallee
					if traceCtx.Ctx != nil {
						traceCtx.Ctx.Txid = traceCtx.MCallee
					}
				}

			case conf.TraceMtraceSpecKey1:
				arr := stringutil.Split(v, ",")
				if len(arr) >= 2 {
					traceCtx.MCallerSpec = arr[0]
					traceCtx.MCallerUrl = arr[1]
				}
			case conf.TraceMtracePoidKey:
				traceCtx.MCallerPoidKey = v
			}
		}
	}

	if traceCtx.MTid == 0 {
		checkSeq := keygen.Next()
		if int32(math.Abs(float64(checkSeq/100%100))) < conf.MtraceRate {
			traceCtx.MTid = checkSeq
		}
	}
	traceCtx.TraceMtraceCallerValue = fmt.Sprintf("%s,%s,%s", hexa32.ToString32(traceCtx.MTid), strconv.Itoa(int(traceCtx.MDepth)+1), hexa32.ToString32(traceCtx.Txid))
	traceCtx.TraceMtraceSpecValue = fmt.Sprintf("%s, %s", conf.MtraceSpec, strconv.Itoa(int(hash.HashStr(traceCtx.Name))))
	traceCtx.TraceMtracePoidValue = fmt.Sprintf("%s, %s, %s", hexa32.ToString32(conf.PCODE), hexa32.ToString32(int64(conf.OKIND)), hexa32.ToString32(conf.OID))
}

// wrapping type of http.HanderFunc, example : http.Handle(pattern, http.HandlerFunc)
func HandlerFunc(handler func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return http.HandlerFunc(Func(handler))
}

// wrapping handler function, example : http.HandleFunc(func(http.ResponseWriter, *http.Request))
func Func(handler func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		conf := agentconfig.GetConfig()
		if !conf.TransactionEnabled {
			handler(w, r)
			return
		}
		wrw := &WrapResponseWriter{ResponseWriter: w}
		ctx, _ := StartWithRequest(r)
		wRequest := r.WithContext(ctx)
		defer func() {
			x := recover()
			var err error = nil
			if x != nil {
				err = fmt.Errorf("%v", x)
				Error(ctx, err)
				err = nil
			}
			status := wrw.Status
			if _, traceCtx := GetTraceContext(ctx); traceCtx != nil {
				traceCtx.Status = int32(status)
			}
			if status >= 400 {
				err = fmt.Errorf("Status %d:%s", status, http.StatusText(status))
			}
			// trace http parameter
			if conf.ProfileHttpParameterEnabled && strings.HasPrefix(r.RequestURI, conf.ProfileHttpParameterUrlPrefix) {
				if wRequest.Form != nil {
					SetParameter(ctx, wRequest.Form)
				}
			}

			// Set Whatap Cookie
			// if conf.TraceUserSetCookie {
			// 	if cookie, exists := GetWhatapCookie(r); !exists {
			// 		SetWhatapCookie(w, cookie)
			// 	}
			// }
			End(ctx, err)
			if x != nil {
				if !conf.GoRecoverEnabled {
					panic(x)
				}
			}
		}()
		handler(wrw, wRequest)

	}
}

func GetTxid(ctx context.Context) int64 {
	if _, traceCtx := GetTraceContext(ctx); traceCtx != nil {
		return traceCtx.Txid
	}
	return 0
}

func GetGID() int64 {
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	b = b[:bytes.IndexByte(b, ' ')]
	n, _ := strconv.ParseUint(string(b), 10, 64)
	return int64(n)
}
func ParseParameter(m map[string][]string) string {
	rt := ""
	if m != nil && len(m) > 0 {
		sb := stringutil.NewStringBuffer()
		for k, v := range m {
			sb.Append(k).Append("=")
			if len(v) > 0 {
				sb.AppendLine(v[0])
			}
		}
		rt = sb.ToString()
		sb.Clear()
	}
	return rt
}

func ParseHeader(m map[string][]string) string {
	conf := agentconfig.GetConfig()
	rt := ""
	if m != nil && len(m) > 0 {
		sb := stringutil.NewStringBuffer()
		for k, v := range m {
			sb.Append(k).Append("=")
			if len(v) > 0 {
				key := strings.ReplaceAll(strings.ToLower(k), "-", "_")
				if !conf.ProfileHttpHeaderIgnoreKeys.HasKey(key) {
					sb.AppendLine(v[0])
				} else {
					sb.AppendLine("#")
				}
			}
		}
		rt = sb.ToString()
		sb.Clear()
	}
	return rt
}
