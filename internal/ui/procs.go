package ui

// 服务器会话（对 Navicat「会话管理」/ DBeaver Sessions 的 TUI 落地）：
//
// 内核的可选能力 ProcessLister（dbcore v0.5.0）——连接就绪后由 app 注入，
// 支持则 `:procs` 列出服务器上**其他会话**（Enter 终止选中，两步确认；
// 杀会话是破坏性操作），不支持给一行说明。

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Ben-guanzhi/dbcore"
)

// errProcNotSupported 是会话管理不可用的说明。
var errProcNotSupported = errors.New("当前引擎不支持会话管理（需要 MySQL / PostgreSQL / SQL Server）")

// procLister 是当前 backend 的进程列表能力（连接就绪后注入；nil = 不支持）。
var (
	procMu     sync.Mutex
	procLister dbcore.ProcessLister
	svReader   dbcore.ServerVariablesReader
)

// driverExposed 是「能暴露内核驱动」的 backend 的小接口——core.Backend 实现
// 它，oobe/host 等其它 Backend 不必（能力断言失败 = 不支持，自然降级）。
type driverExposed interface {
	Driver() dbcore.SQLDriver
}

// SetProcessListerForBackend 在连接就绪/SetBackend 时调用：backend 能暴露
// 内核驱动则断言 ProcessLister + ServerVariables 能力，否则清空（不支持）。
func SetProcessListerForBackend(b any) {
	procMu.Lock()
	defer procMu.Unlock()
	procLister = nil
	svReader = nil
	if d, ok := b.(driverExposed); ok {
		drv := d.Driver()
		if pl, ok := dbcore.AsProcessLister(drv); ok {
			procLister = pl
		}
		if sv, ok := dbcore.AsServerVariables(drv); ok {
			svReader = sv
		}
	}
}

// ListServerVariables 拉当前服务器的系统变量（15s 超时；只读）。
func ListServerVariables() ([]dbcore.ServerVariable, error) {
	procMu.Lock()
	sv := svReader
	procMu.Unlock()
	if sv == nil {
		return nil, errProcNotSupported
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	vars, err := sv.ServerVariables(ctx)
	dbcore.SortVariables(vars)
	return vars, err
}

// SetProcessLister 直接注入能力（测试用）。
func SetProcessLister(l dbcore.ProcessLister) {
	procMu.Lock()
	defer procMu.Unlock()
	procLister = l
}

// ListServerProcesses 拉当前会话列表（10s 超时；不含当前连接）。
func ListServerProcesses() ([]dbcore.ServerProcess, error) {
	procMu.Lock()
	l := procLister
	procMu.Unlock()
	if l == nil {
		return nil, errProcNotSupported
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return l.Processes(ctx)
}

// KillServerProcess 终止一个会话（ID 只允许数字——内核校验，这里不重复）。
func KillServerProcess(id string) error {
	procMu.Lock()
	l := procLister
	procMu.Unlock()
	if l == nil {
		return errProcNotSupported
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return l.KillProcess(ctx, id)
}
