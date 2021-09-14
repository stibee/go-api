package udp

import (
	"fmt"

	"github.com/whatap/go-api/common/io"
	"github.com/whatap/go-api/common/util/paramtext"
)

type UdpTxSqlParamPack struct {
	AbstractPack
	Dbc   string
	Sql   string
	Param string
	//error
	ErrorType    string
	ErrorMessage string

	Stack string
}

func NewUdpTxSqlParamPack() *UdpTxSqlParamPack {
	p := new(UdpTxSqlParamPack)
	p.Ver = UDP_PACK_VERSION
	p.AbstractPack.Flush = false
	return p
}

func NewUdpTxSqlParamPackVer(ver int32) *UdpTxSqlParamPack {
	p := new(UdpTxSqlParamPack)
	p.Ver = ver
	p.AbstractPack.Flush = false
	return p
}

func (this *UdpTxSqlParamPack) GetPackType() uint8 {
	return TX_SQL_PARAM
}

func (this *UdpTxSqlParamPack) ToString() string {
	return fmt.Sprint(this.AbstractPack.ToString(), ",dbc=", this.Dbc, ",sql=", this.Sql, ",desc=")
}

func (this *UdpTxSqlParamPack) Clear() {
	this.AbstractPack.Clear()
	this.AbstractPack.Flush = false

	this.Dbc = ""
	this.Sql = ""
	//error
	this.ErrorType = ""
	this.ErrorMessage = ""

	this.Stack = ""
	this.Param = ""
}

func (this *UdpTxSqlParamPack) Write(dout *io.DataOutputX) {
	this.AbstractPack.Write(dout)
	dout.WriteTextShortLength(this.Dbc)
	dout.WriteTextShortLength(this.Sql)
	dout.WriteTextShortLength(this.Param)

	if this.Ver > 40000 {
		// Batch
	} else if this.Ver > 30000 {
		// Dotnet
	} else if this.Ver > 20000 {
		// Python
	} else {
		// PHP
		if this.Ver >= 10105 {
			dout.WriteTextShortLength(this.ErrorType)
			dout.WriteTextShortLength(this.ErrorMessage)
			dout.WriteTextShortLength(this.Stack)
		}
	}
}

func (this *UdpTxSqlParamPack) Read(din *io.DataInputX) {
	this.AbstractPack.Read(din)

	this.Dbc = din.ReadTextShortLength()
	this.Sql = din.ReadTextShortLength()
	this.Param = din.ReadTextShortLength()

	if this.Ver > 40000 {
		// Batch
	} else if this.Ver > 30000 {
		// Dotnet
	} else if this.Ver > 20000 {
		// Python
	} else {
		// PHP
		if this.Ver >= 10105 {
			this.ErrorType = din.ReadTextShortLength()
			this.ErrorMessage = din.ReadTextShortLength()
			this.Stack = din.ReadTextShortLength()
		}
	}
}

func (this *UdpTxSqlParamPack) Process() {
	if this.Ver > 40000 {
		// Batch
	} else if this.Ver > 30000 {
		// Dotnet
	} else if this.Ver > 20000 {
		// Python
	} else {
		// PHP
		if this.Dbc != "" {
			p := paramtext.NewParamKVSeperate(this.Dbc, " ", "=")
			this.Dbc = p.ToStringStr("password", "#")
			p = paramtext.NewParamKVSeperate(this.Dbc, ";", "=")
			this.Dbc = p.ToStringStr("password", "#")
		}
		if len(this.Sql) >= UDP_PACKET_SQL_MAX_SIZE {
			this.Sql = "[QUERY TOO LONG]\r\n" + this.Sql
		}
	}
}
