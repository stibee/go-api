package whatapsql

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/go-sql-driver/mysql"
	"strings"
	"time"

	//"fmt"
	//"runtime/debug"

	"github.com/whatap/go-api/agent/agent/config"
	whatapsql "github.com/whatap/go-api/sql"
	"github.com/whatap/go-api/trace"
	"github.com/whatap/golib/util/dateutil"
)

type dsnConnector struct {
	dsn    string
	driver driver.Driver
}

func (t dsnConnector) Connect(_ context.Context) (driver.Conn, error) {
	return t.driver.Open(t.dsn)
}

func (t dsnConnector) Driver() driver.Driver {
	return t.driver
}

type WhatapDriver struct {
	driver.Driver
	ctx context.Context
}

func (d WhatapDriver) Open(name string) (driver.Conn, error) {
	return d.Driver.Open(name)
}

func (d WhatapDriver) OpenConnector(name string) (c driver.Connector, err error) {
	if dCtx, ok := d.Driver.(driver.DriverContext); ok {
		c, err = dCtx.OpenConnector(name)
		if err != nil {
			return nil, err
		}
		return WrapConnector{c, d.ctx, name}, nil
	}
	return driver.Connector(WrapConnector{dsnConnector{name, d}, d.ctx, name}), nil
}

func (d WhatapDriver) OpenConnectorContext(name string, ctx context.Context) (c driver.Connector, err error) {
	if dCtx, ok := d.Driver.(driver.DriverContext); ok {
		c, err = dCtx.OpenConnector(name)
		if err != nil {
			return nil, err
		}
		return WrapConnector{c, ctx, name}, nil
	}
	return driver.Connector(WrapConnector{dsnConnector{name, d}, ctx, name}), nil
}

type WrapConnector struct {
	driver.Connector
	ctx            context.Context
	dataSourceName string
}

func (ct WrapConnector) Connect(ctx context.Context) (c driver.Conn, err error) {
	conf := config.GetConfig()
	if !conf.GoSqlProfileEnabled {
		return ct.Connector.Connect(ctx)
	}

	wCtx := selectContext(ctx, ct.ctx)
	sqlCtx, _ := whatapsql.StartOpen(wCtx, ct.dataSourceName)

	for i := 0; i < 5; i++ {
		c, err = ct.Connector.Connect(ctx)
		if errors.Is(err, mysql.ErrInvalidConn) || strings.Contains(err.Error(), "No connection could be made because the target machine actively refused it") {
			fmt.Println("[mysql] retrying", err)
			time.Sleep(time.Second * 5)
			continue
		}
		break
	}
	whatapsql.End(sqlCtx, err)
	if err != nil {
		return nil, err
	}
	return driver.Conn(WrapConn{c, wCtx, ct.dataSourceName}), err
}

type WrapConn struct {
	driver.Conn
	ctx            context.Context
	dataSourceName string
}

func (c WrapConn) Exec(query string, args []driver.Value) (res driver.Result, err error) {
	if exec, ok := c.Conn.(driver.Execer); ok {
		sqlCtx, _ := whatapsql.StartWithParam(c.ctx, c.dataSourceName, query, convertDriverValue(args)...)
		for i := 0; i < 5; i++ {
			res, err = exec.Exec(query, args)
			if errors.Is(err, mysql.ErrInvalidConn) || strings.Contains(err.Error(), "No connection could be made because the target machine actively refused it") {
				fmt.Println("[mysql] retrying", err)
				time.Sleep(time.Second * 5)
				continue
			}
			break
		}
		whatapsql.End(sqlCtx, err)
		return res, err
	}
	return nil, driver.ErrSkip
}

func (c WrapConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (res driver.Result, err error) {
	wCtx := selectContext(ctx, c.ctx)
	if execCtx, ok := c.Conn.(driver.ExecerContext); ok {
		sqlCtx, _ := whatapsql.StartWithParam(wCtx, c.dataSourceName, query, convertDriverNamedValue(args)...)
		for i := 0; i < 5; i++ {
			res, err = execCtx.ExecContext(ctx, query, args)
			if errors.Is(err, mysql.ErrInvalidConn) || strings.Contains(err.Error(), "No connection could be made because the target machine actively refused it") {
				fmt.Println("[mysql] retrying", err)
				time.Sleep(time.Second * 5)
				continue
			}
			break
		}
		whatapsql.End(sqlCtx, err)
		return res, err
	}
	return nil, driver.ErrSkip
}

func (c WrapConn) Query(query string, args []driver.Value) (rows driver.Rows, err error) {
	if queryer, ok := c.Conn.(driver.Queryer); ok {
		sqlCtx, _ := whatapsql.StartWithParam(c.ctx, c.dataSourceName, query, convertDriverValue(args)...)

		for i := 0; i < 5; i++ {
			rows, err = queryer.Query(query, args)
			if errors.Is(err, mysql.ErrInvalidConn) || strings.Contains(err.Error(), "No connection could be made because the target machine actively refused it") {
				fmt.Println("[mysql] retrying", err)
				time.Sleep(time.Second * 5)
				continue
			}
			break
		}
		whatapsql.End(sqlCtx, err)
		return
	}
	return nil, driver.ErrSkip
}

func (c WrapConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (rows driver.Rows, err error) {
	wCtx := selectContext(ctx, c.ctx)
	if queryerCtx, ok := c.Conn.(driver.QueryerContext); ok {
		sqlCtx, _ := whatapsql.StartWithParam(wCtx, c.dataSourceName, query, convertDriverNamedValue(args)...)
		for i := 0; i < 5; i++ {
			rows, err = queryerCtx.QueryContext(ctx, query, args)
			if errors.Is(err, mysql.ErrInvalidConn) || strings.Contains(err.Error(), "No connection could be made because the target machine actively refused it") {
				fmt.Println("[mysql] retrying", err)
				time.Sleep(time.Second * 5)
				continue
			}
			break
		}
		whatapsql.End(sqlCtx, err)
		return
	}
	return nil, driver.ErrSkip
}
func (c WrapConn) Prepare(query string) (stmt driver.Stmt, err error) {
	for i := 0; i < 5; i++ {
		stmt, err = c.Conn.Prepare(query)
		if errors.Is(err, mysql.ErrInvalidConn) || strings.Contains(err.Error(), "No connection could be made because the target machine actively refused it") {
			fmt.Println("[mysql] retrying", err)
			time.Sleep(time.Second * 5)
			continue
		}
		break
	}

	if err != nil {
		return nil, err
	}
	return driver.Stmt(WrapStmt{stmt, c.ctx, c.dataSourceName, query}), err
}

func (c WrapConn) PrepareContext(ctx context.Context, query string) (stmt driver.Stmt, err error) {
	wCtx := selectContext(ctx, c.ctx)
	if prepCtx, ok := c.Conn.(driver.ConnPrepareContext); ok {
		for i := 0; i < 5; i++ {
			stmt, err = prepCtx.PrepareContext(ctx, query)
			if errors.Is(err, mysql.ErrInvalidConn) || strings.Contains(err.Error(), "No connection could be made because the target machine actively refused it") {
				fmt.Println("[mysql] retrying", err)
				time.Sleep(time.Second * 5)
				continue
			}
			break
		}
	} else {
		for i := 0; i < 5; i++ {
			stmt, err = c.Conn.Prepare(query)
			if errors.Is(err, mysql.ErrInvalidConn) || strings.Contains(err.Error(), "No connection could be made because the target machine actively refused it") {
				fmt.Println("[mysql] retrying", err)
				time.Sleep(time.Second * 5)
				continue
			}
			break
		}
	}
	if err != nil {
		return nil, err
	}
	return driver.Stmt(WrapStmt{stmt, wCtx, c.dataSourceName, query}), err
}

func (c WrapConn) Close() error {
	st := dateutil.SystemNow()
	err := c.Conn.Close()
	elapsed := dateutil.SystemNow() - st
	if elapsed < 0 {
		elapsed = 0
	}
	trace.Step(c.ctx, "Close", "Close", int(elapsed), 0)
	if err != nil {
		trace.Error(c.ctx, err)
	}
	return err
}

func (c WrapConn) ResetSession(ctx context.Context) error {
	if cr, ok := c.Conn.(driver.SessionResetter); ok {
		return cr.ResetSession(ctx)
	}
	return nil
}
func (c WrapConn) Begin() (tx driver.Tx, err error) {
	st := dateutil.SystemNow()
	for i := 0; i < 5; i++ {
		tx, err = c.Conn.Begin()
		if errors.Is(err, mysql.ErrInvalidConn) || strings.Contains(err.Error(), "No connection could be made because the target machine actively refused it") {
			fmt.Println("[mysql] retrying", err)
			time.Sleep(time.Second * 5)
			continue
		}
		break
	}
	elapsed := dateutil.SystemNow() - st
	if elapsed < 0 {
		elapsed = 0
	}
	trace.Step(c.ctx, "Begin", "Begin", int(elapsed), 0)
	if err != nil {
		trace.Error(c.ctx, err)
		return nil, err
	}
	return WrapTx{tx, c.ctx, c.dataSourceName}, nil
}

func (c WrapConn) BeginTx(ctx context.Context, opts driver.TxOptions) (tx driver.Tx, err error) {
	wCtx := selectContext(ctx, c.ctx)
	if connBeginTx, ok := c.Conn.(driver.ConnBeginTx); ok {
		st := dateutil.SystemNow()
		for i := 0; i < 5; i++ {
			tx, err = connBeginTx.BeginTx(ctx, opts)
			if errors.Is(err, mysql.ErrInvalidConn) || strings.Contains(err.Error(), "No connection could be made because the target machine actively refused it") {
				fmt.Println("[mysql] retrying", err)
				time.Sleep(time.Second * 5)
				continue
			}
			break
		}
		elapsed := dateutil.SystemNow() - st
		if elapsed < 0 {
			elapsed = 0
		}
		trace.Step(wCtx, "BeginTx", "BeginTx", int(elapsed), 0)
		if err != nil {
			trace.Error(wCtx, err)
			return nil, err
		}
		return WrapTx{tx, wCtx, c.dataSourceName}, nil
	}
	for i := 0; i < 5; i++ {
		tx, err = c.Conn.Begin()
		if errors.Is(err, mysql.ErrInvalidConn) || strings.Contains(err.Error(), "No connection could be made because the target machine actively refused it") {
			fmt.Println("[mysql] retrying", err)
			time.Sleep(time.Second * 5)
			continue
		}
		break
	}
	if err != nil {
		return nil, err
	}
	return WrapTx{tx, wCtx, c.dataSourceName}, nil
}

type WrapStmt struct {
	driver.Stmt
	ctx            context.Context
	dataSourceName string
	preparedSql    string
}

func (s WrapStmt) Exec(args []driver.Value) (res driver.Result, err error) {
	sqlCtx, _ := whatapsql.StartWithParam(s.ctx, s.dataSourceName, s.preparedSql, convertDriverValue(args)...)
	for i := 0; i < 5; i++ {
		res, err = s.Stmt.Exec(args)
		if errors.Is(err, mysql.ErrInvalidConn) || strings.Contains(err.Error(), "No connection could be made because the target machine actively refused it") {
			fmt.Println("[mysql] retrying", err)
			time.Sleep(time.Second * 5)
			continue
		}
		break
	}
	whatapsql.End(sqlCtx, err)
	return res, err
}

func (s WrapStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (res driver.Result, err error) {
	wCtx := selectContext(ctx, s.ctx)
	if execCtx, ok := s.Stmt.(driver.StmtExecContext); ok {
		sqlCtx, _ := whatapsql.StartWithParam(wCtx, s.dataSourceName, s.preparedSql, convertDriverNamedValue(args)...)
		for i := 0; i < 5; i++ {
			res, err = execCtx.ExecContext(ctx, args)
			if errors.Is(err, mysql.ErrInvalidConn) || strings.Contains(err.Error(), "No connection could be made because the target machine actively refused it") {
				fmt.Println("[mysql] retrying", err)
				time.Sleep(time.Second * 5)
				continue
			}
			break
		}
		whatapsql.End(sqlCtx, err)
		return res, err
	}
	dargs, err := namedValueToValue(args)
	if err != nil {
		return nil, err
	}
	return s.Stmt.Exec(dargs)
}

func (s WrapStmt) Query(args []driver.Value) (rows driver.Rows, err error) {
	sqlCtx, _ := whatapsql.StartWithParam(s.ctx, s.dataSourceName, s.preparedSql, convertDriverValue(args)...)
	for i := 0; i < 5; i++ {
		rows, err = s.Stmt.Query(args)
		if errors.Is(err, mysql.ErrInvalidConn) || strings.Contains(err.Error(), "No connection could be made because the target machine actively refused it") {
			fmt.Println("[mysql] retrying", err)
			time.Sleep(time.Second * 5)
			continue
		}
		break
	}
	whatapsql.End(sqlCtx, err)
	return
}

func (s WrapStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (rows driver.Rows, err error) {
	wCtx := selectContext(ctx, s.ctx)
	if queryerCtx, ok := s.Stmt.(driver.StmtQueryContext); ok {
		sqlCtx, _ := whatapsql.StartWithParam(wCtx, s.dataSourceName, s.preparedSql, convertDriverNamedValue(args)...)
		for i := 0; i < 5; i++ {
			rows, err = queryerCtx.QueryContext(ctx, args)
			if errors.Is(err, mysql.ErrInvalidConn) || strings.Contains(err.Error(), "No connection could be made because the target machine actively refused it") {
				fmt.Println("[mysql] retrying", err)
				time.Sleep(time.Second * 5)
				continue
			}
			break
		}
		whatapsql.End(sqlCtx, err)
		return
	}
	dargs, err := namedValueToValue(args)
	if err != nil {
		return nil, err
	}
	return s.Stmt.Query(dargs)
}

type WrapTx struct {
	driver.Tx
	ctx            context.Context
	dataSourceName string
}

func (t WrapTx) Commit() (err error) {
	st := dateutil.SystemNow()
	err = t.Tx.Commit()
	elapsed := dateutil.SystemNow() - st
	if elapsed < 0 {
		elapsed = 0
	}
	trace.Step(t.ctx, "Commit", "Commit", int(elapsed), 0)
	if err != nil {
		trace.Error(t.ctx, err)
	}
	return err
}

func (t WrapTx) Rollback() (err error) {
	st := dateutil.SystemNow()
	err = t.Tx.Rollback()
	elapsed := dateutil.SystemNow() - st
	if elapsed < 0 {
		elapsed = 0
	}
	trace.Step(t.ctx, "Rollback", "Rollback", int(elapsed), 0)
	if err != nil {
		trace.Error(t.ctx, err)
	}
	return err
}

func convertDriverValue(args []driver.Value) []interface{} {
	iArgs := make([]interface{}, 0)
	for _, it := range args {
		iArgs = append(iArgs, interface{}(it))
	}
	return iArgs
}

func convertDriverNamedValue(args []driver.NamedValue) []interface{} {
	iArgs := make([]interface{}, 0)
	for _, it := range args {
		iArgs = append(iArgs, it)
	}
	return iArgs
}

func namedValueToValue(named []driver.NamedValue) ([]driver.Value, error) {
	dargs := make([]driver.Value, len(named))
	for n, param := range named {
		if len(param.Name) > 0 {
			return nil, errors.New("sql: driver does not support the use of Named Parameters")
		}
		dargs[n] = param.Value
	}
	return dargs, nil
}

func selectContext(contexts ...context.Context) (ctx context.Context) {
	var first context.Context
	for i, it := range contexts {
		if i == 0 {
			first = it
		}
		if _, traceCtx := trace.GetTraceContext(it); traceCtx != nil {
			return it
		}
	}
	return first
}
