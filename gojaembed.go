package main

import (
    "bytes"
    "io/ioutil"
    "net/url"
    "os"
    "strconv"
    "sync/atomic"

    "github.com/go-sourcemap/sourcemap"
    "github.com/packing/goja"
    "github.com/packing/goja_nodejs/require"
    "github.com/packing/nbpy/codecs"
    "github.com/packing/nbpy/utils"
)

var LogLevelAssert = utils.LogLevelError + 1

var OnGojaSendMessage func(string, uint64, interface{}) int = nil
var OnGojaSendMessageTo func(interface{}) int = nil

var gojaRequire = new(require.Registry)

type Util struct {
    runtime *goja.Runtime
}

func (u *Util) format(f rune, val goja.Value, w *bytes.Buffer) bool {
    switch f {
    case 's':
        w.WriteString(val.String())
    case 'd':
        w.WriteString(val.ToNumber().String())
    case 'j':
        if json, ok := u.runtime.Get("JSON").(*goja.Object); ok {
            if stringify, ok := goja.AssertFunction(json.Get("stringify")); ok {
                res, err := stringify(json, val)
                if err != nil {
                    panic(err)
                }
                w.WriteString(res.String())
            }
        }
    case '%':
        w.WriteByte('%')
        return false
    default:
        w.WriteByte('%')
        w.WriteRune(f)
        return false
    }
    return true
}

func (u *Util) Format(b *bytes.Buffer, f string, args ...goja.Value) {
    pct := false
    argNum := 0
    for _, chr := range f {
        if pct {
            if argNum < len(args) {
                if u.format(chr, args[argNum], b) {
                    argNum++
                }
            } else {
                b.WriteByte('%')
                b.WriteRune(chr)
            }
            pct = false
        } else {
            if chr == '%' {
                pct = true
            } else {
                b.WriteRune(chr)
            }
        }
    }

    for _, arg := range args[argNum:] {
        b.WriteByte(' ')
        b.WriteString(arg.String())
    }
}

type Console struct {
    runtime *goja.Runtime
    util    *Util
}

func (c *Console) formatArgs(args []goja.Value) string {
    var b bytes.Buffer
    var fmt string

    if arg := args[0]; !goja.IsUndefined(arg) {
        fmt = arg.String()
    }

    var fargs = args[1:]
    c.util.Format(&b, fmt, fargs...)

    return "[J] " + b.String()
}

func (c *Console) log(logLevel int) func(goja.FunctionCall) goja.Value {
    ret := func(call goja.FunctionCall) goja.Value {
        if len(call.Arguments) == 0 {
            return goja.Undefined()
        }

        if logLevel == LogLevelAssert {
            if len(call.Arguments) < 2 {
                return goja.Undefined()
            }
            if call.Arguments[0].ToBoolean() {
                utils.LogError(c.formatArgs(call.Arguments[1:]))
            }
            return goja.Undefined()
        }

        if logLevel == utils.LogLevelVerbose {
            utils.LogVerbose(c.formatArgs(call.Arguments))
        }
        if logLevel == utils.LogLevelWarn {
            utils.LogWarn(c.formatArgs(call.Arguments))
        }
        if logLevel == utils.LogLevelError {
            utils.LogError(c.formatArgs(call.Arguments))
        }
        if logLevel == utils.LogLevelInfo {
            utils.LogInfo(c.formatArgs(call.Arguments))
        }
        return goja.Undefined()
    }
    return ret
}

func requireConsole(runtime *goja.Runtime, module *goja.Object) {
    c := &Console{
        runtime: runtime,
        util:    &Util{runtime: runtime},
    }

    o := module.Get("exports").(*goja.Object)
    o.Set("log", c.log(utils.LogLevelVerbose))
    o.Set("assert", c.log(LogLevelAssert))
    o.Set("error", c.log(utils.LogLevelError))
    o.Set("warn", c.log(utils.LogLevelWarn))
    o.Set("info", c.log(utils.LogLevelInfo))
}

func EnableConsole(runtime *goja.Runtime) {
    runtime.Set("console", require.Require(runtime, "console"))
}

type GojaVMNet struct {
    vm *GojaVM
}

func transferGojaArray2GoArray(goArray []interface{}) []interface{} {
    newArray := make([]interface{}, len(goArray))
    for i, v := range goArray {
        switch v.(type) {
        case float64:
            if float64(int64(v.(float64))) == v {
                newArray[i] = int64(v.(float64))
            } else if float64(uint64(v.(float64))) == v {
                newArray[i] = uint64(v.(float64))
            }
        case float32:
            if float32(int32(v.(float32))) == v {
                newArray[i] = int32(v.(float32))
            } else if float32(uint32(v.(float32))) == v {
                newArray[i] = uint32(v.(float32))
            }
        case map[string]interface{}:
            newArray[i] = transferGojaMap2GoMap(v.(map[string]interface{}))
        case []interface{}:
            newArray[i] = transferGojaArray2GoArray(v.([]interface{}))
        default:
            newArray[i] = v
        }
    }
    return newArray
}

func transferGojaMap2GoMap(goMap map[string]interface{}) map[interface{}]interface{} {
    out := make(map[interface{}]interface{})
    for k, v := range goMap {

        rv := v
        switch rv.(type) {
        case float64:
            if float64(int64(rv.(float64))) == rv {
                rv = int64(rv.(float64))
            } else if float64(uint64(rv.(float64))) == rv {
                rv = uint64(rv.(float64))
            }
        case float32:
            if float32(int32(rv.(float32))) == rv {
                rv = int32(rv.(float32))
            } else if float32(uint32(rv.(float32))) == rv {
                rv = uint32(rv.(float32))
            }
        case map[string]interface{}:
            rv = transferGojaMap2GoMap(v.(map[string]interface{}))
        case []interface{}:
            rv = transferGojaArray2GoArray(v.([]interface{}))
        }

        rk, err := strconv.ParseInt(k, 0, 64)
        if err != nil {
            out[k] = rv
        } else {
            out[rk] = rv
        }
    }
    return out
}

func (n GojaVMNet) SendCurrentPlayer(call goja.FunctionCall) goja.Value {
    if OnGojaSendMessage == nil {
        return n.vm.Runtime.ToValue(-1)
    }
    if len(call.Arguments) == 0 {
        return n.vm.Runtime.ToValue(-1)
    }
    if goja.IsUndefined(call.Arguments[0]) || goja.IsNull(call.Arguments[0]) {
        return n.vm.Runtime.ToValue(-1)
    }

    im := call.Arguments[0].Export()
    if im == nil {
        return n.vm.Runtime.ToValue(-1)
    }

    m, ok := im.(map[string]interface{})
    if !ok {
        return n.vm.Runtime.ToValue(-1)
    }

    sm := transferGojaMap2GoMap(m)

    sAddr := n.vm.associatedSourceAddr
    sId := n.vm.associatedSourceId
    OnGojaSendMessage(sAddr, sId, sm)

    return n.vm.Runtime.ToValue(0)
}

func (n GojaVMNet) SendToOtherPlayer(call goja.FunctionCall) goja.Value {
    if OnGojaSendMessage == nil {
        return n.vm.Runtime.ToValue(-1)
    }
    if len(call.Arguments) == 0 {
        return n.vm.Runtime.ToValue(-1)
    }
    if goja.IsUndefined(call.Arguments[0]) || goja.IsNull(call.Arguments[0]) {
        return n.vm.Runtime.ToValue(-1)
    }

    im := call.Arguments[0].Export()
    if im == nil {
        return n.vm.Runtime.ToValue(-1)
    }

    m, ok := im.(map[string]interface{})
    if !ok {
        return n.vm.Runtime.ToValue(-1)
    }

    sm := transferGojaMap2GoMap(m)

    OnGojaSendMessageTo(sm)

    return n.vm.Runtime.ToValue(0)
}

func (n GojaVMNet) InitLock(call goja.FunctionCall) goja.Value {
    if globalStorage == nil {
        return n.vm.Runtime.ToValue(false)
    }

    if len(call.Arguments) == 0 || goja.IsUndefined(call.Arguments[0]) || goja.IsNull(call.Arguments[0]) {
        return n.vm.Runtime.ToValue(false)
    }

    key := uint64(call.Arguments[0].ToInteger())

    ok := globalStorage.InitLock(key)
    return n.vm.Runtime.ToValue(ok)
}

func (n GojaVMNet) DisposeLock(call goja.FunctionCall) goja.Value {
    if globalStorage == nil {
        return n.vm.Runtime.ToValue(false)
    }

    if len(call.Arguments) == 0 || goja.IsUndefined(call.Arguments[0]) || goja.IsNull(call.Arguments[0]) {
        return n.vm.Runtime.ToValue(false)
    }

    key := uint64(call.Arguments[0].ToInteger())

    ok := globalStorage.DisposeLock(key)
    return n.vm.Runtime.ToValue(ok)
}

func (n GojaVMNet) Lock(call goja.FunctionCall) goja.Value {
    if globalStorage == nil {
        return n.vm.Runtime.ToValue(-1)
    }


    key := n.vm.defKeyForLock
    if len(call.Arguments) > 0 && !goja.IsUndefined(call.Arguments[0]) && !goja.IsNull(call.Arguments[0]) {
        key = uint64(call.Arguments[0].ToInteger())
    }

    /* 此处有争议，单连接可以重复发起加锁请求，因为消息是异步并发的，所以同时有两个消息进了加锁然后都还没到解锁这一步是正常存在的？
    if key == n.vm.defKeyForLock && n.vm.sidForLock > 0 {
        stacks := make([]goja.StackFrame, 5)

        errStr := GenGojaStackFrameString(n.vm,"[J] !!! Repeat global lock request", n.vm.Runtime.CaptureCallStack(5, stacks))
        utils.LogError(errStr)

        n.vm.Runtime.Interrupt("halt")
        return n.vm.Runtime.ToValue(-1)
    }*/

    sid, ok := globalStorage.Lock(key)
    if ok {
        atomic.AddUint64(&locklogic, 1)
        n.vm.sidForLock = sid
        return n.vm.Runtime.ToValue(sid)
    } else {
        n.vm.sidForLock = 0
    }

    return n.vm.Runtime.ToValue(0)
}

func (n GojaVMNet) Unlock(call goja.FunctionCall) goja.Value {
    if globalStorage == nil {
        return n.vm.Runtime.ToValue(-1)
    }

    key := n.vm.defKeyForLock
    sid := n.vm.sidForLock
    if len(call.Arguments) > 0 && !goja.IsUndefined(call.Arguments[0]) && !goja.IsNull(call.Arguments[0]) {
        key = uint64(call.Arguments[0].ToInteger())
    }
    if len(call.Arguments) > 1 && !goja.IsUndefined(call.Arguments[1]) && !goja.IsNull(call.Arguments[1]) {
        sid = call.Arguments[1].ToInteger()
    }

    if key == n.vm.defKeyForLock && n.vm.sidForLock == 0 {
        //utils.LogError("[J] !!! ==> 此时无法归还默认全局锁, 是否在之前并未获取过默认全局锁?")
        return n.vm.Runtime.ToValue(-1)
    }

    if globalStorage.Unlock(sid, key) {
        atomic.AddUint64(&unlocklogic, 1)
        n.vm.sidForLock = 0
        return n.vm.Runtime.ToValue(0)
    } else {
        n.vm.sidForLock = 0
        return n.vm.Runtime.ToValue(-1)
    }

}

func (n GojaVMNet) Query(call goja.FunctionCall) goja.Value {
    if globalStorage == nil {
        return goja.Null()
    }

    if len(call.Arguments) == 0 {
        return goja.Null()
    }

    sql := call.Arguments[0].String()
    args := make([]interface{}, 0)
    if len(call.Arguments) > 1 {
        for _, a := range call.Arguments[1:] {
            args = append(args, a.Export())
        }
    }

    rows, err := globalStorage.DBQuery(sql, args...)
    if err != nil {
        return goja.Null()
    }

    toRows := transferGoArray2GojaArray(rows)

    return n.vm.Runtime.ToValue(toRows)
}

func (n GojaVMNet) Exec(call goja.FunctionCall) goja.Value {
    if globalStorage == nil {
        return n.vm.Runtime.ToValue(0)
    }

    if len(call.Arguments) == 0 {
        return n.vm.Runtime.ToValue(0)
    }

    sql := call.Arguments[0].String()
    args := make([]interface{}, 0)
    if len(call.Arguments) > 1 {
        for _, a := range call.Arguments[1:] {
            args = append(args, a.Export())
        }
    }

    en, err := globalStorage.DBExec(sql, args...)
    if err != nil {
        return n.vm.Runtime.ToValue(0)
    }

    return n.vm.Runtime.ToValue(en)
}

func (n GojaVMNet) Transaction(call goja.FunctionCall) goja.Value {
    if globalStorage == nil {
        return n.vm.Runtime.ToValue(false)
    }

    return n.vm.Runtime.ToValue(false)
}

func (n GojaVMNet) Open(call goja.FunctionCall) goja.Value {
    if globalStorage == nil {
        return n.vm.Runtime.ToValue(false)
    }

    if n.vm.defKeyForRedis > 0 {
        stacks := make([]goja.StackFrame, 5)
        errStr := GenGojaStackFrameString(n.vm,"[J] !!! Redis has been opened", n.vm.Runtime.CaptureCallStack(5, stacks))
        utils.LogError(errStr)
        return n.vm.Runtime.ToValue(false)
    }

    n.vm.defKeyForRedis = n.vm.defKeyForLock
    b := globalStorage.RedisOpen(n.vm.defKeyForRedis)
    return n.vm.Runtime.ToValue(b)
}

func (n GojaVMNet) Close(call goja.FunctionCall) goja.Value {
    if globalStorage == nil {
        return n.vm.Runtime.ToValue(false)
    }

    if n.vm.defKeyForRedis == 0 {
        stacks := make([]goja.StackFrame, 5)
        errStr := GenGojaStackFrameString(n.vm,"[J] !!! Redis has not been opened", n.vm.Runtime.CaptureCallStack(5, stacks))
        utils.LogError(errStr)
        return n.vm.Runtime.ToValue(false)
    }

    b := globalStorage.RedisClose(n.vm.defKeyForRedis)
    if b {
        n.vm.defKeyForRedis = 0
    }
    return n.vm.Runtime.ToValue(b)
}

func (n GojaVMNet) Do(call goja.FunctionCall) goja.Value {
    if globalStorage == nil {
        return goja.Null()
    }

    if len(call.Arguments) == 0 {
        return goja.Null()
    }

    cmd := call.Arguments[0].String()
    args := make([]interface{}, 0)
    if len(call.Arguments) > 1 {
        for _, a := range call.Arguments[1:] {
            args = append(args, a.Export())
        }
    }

    rows := globalStorage.RedisDo(cmd, args...)
    if rows == nil {
        return goja.Null()
    }

    //utils.LogError("Do return >>>", rows)

    switch rows.(type) {
    case string: return n.vm.Runtime.ToValue(rows)
    case []byte: return n.vm.Runtime.ToValue(string(rows.([]byte)))
    case map[interface{}]interface{}:
        return n.vm.Runtime.ToValue(transferGoMap2GojaMap(rows.(map[interface{}]interface{})))
    case []interface{}:
        return n.vm.Runtime.ToValue(transferGoArray2GojaArray(rows.([]interface{})))
    }

    return n.vm.Runtime.ToValue(rows)
}

func (n GojaVMNet) Send(call goja.FunctionCall) goja.Value {
    if globalStorage == nil {
        return n.vm.Runtime.ToValue(false)
    }

    if len(call.Arguments) == 0 {
        return n.vm.Runtime.ToValue(false)
    }

    if n.vm.defKeyForRedis == 0 {
        stacks := make([]goja.StackFrame, 5)
        errStr := GenGojaStackFrameString(n.vm,"[J] !!! You must open the Redis before executing send", n.vm.Runtime.CaptureCallStack(5, stacks))
        utils.LogError(errStr)
        return n.vm.Runtime.ToValue(false)
    }

    cmd := call.Arguments[0].String()
    args := make([]interface{}, 0)
    if len(call.Arguments) > 1 {
        for _, a := range call.Arguments[1:] {
            args = append(args, a.Export())
        }
    }

    b := globalStorage.RedisSend(n.vm.defKeyForRedis, cmd, args...)
    return n.vm.Runtime.ToValue(b)
}

func (n GojaVMNet) Flush(call goja.FunctionCall) goja.Value {
    if globalStorage == nil {
        return n.vm.Runtime.ToValue(false)
    }

    if n.vm.defKeyForRedis == 0 {
        stacks := make([]goja.StackFrame, 5)
        errStr := GenGojaStackFrameString(n.vm,"[J] !!! You must open the Redis before executing flush", n.vm.Runtime.CaptureCallStack(5, stacks))
        utils.LogError(errStr)
        return n.vm.Runtime.ToValue(false)
    }

    b := globalStorage.RedisFlush(n.vm.defKeyForRedis)
    if b {
        n.vm.defKeyForRedis = 0
    }
    return n.vm.Runtime.ToValue(b)
}

func (n GojaVMNet) Receive(call goja.FunctionCall) goja.Value {
    if globalStorage == nil {
        return goja.Null()
    }

    if n.vm.defKeyForRedis == 0 {
        stacks := make([]goja.StackFrame, 5)
        errStr := GenGojaStackFrameString(n.vm,"[J] !!! You must open the Redis before executing receive", n.vm.Runtime.CaptureCallStack(5, stacks))
        utils.LogError(errStr)
        return goja.Null()
    }

    row := globalStorage.RedisReceive(n.vm.defKeyForRedis)
    if row == nil {
        return goja.Null()
    }
    return n.vm.Runtime.ToValue(row)
}

type GojaVM struct {
    Runtime                 *goja.Runtime
    associatedSourceAddr    string
    associatedSourceId      uint64
    defKeyForLock           uint64
    defKeyForRedis          uint64
    sidForLock              int64
    consumer                *sourcemap.Consumer
}

///此channel用来确保只有唯一一个vm上下文的init会被执行
var gojaInitCallbackCh chan int

func GojaInit() {
    gojaInitCallbackCh = make(chan int)
    go func() {
        gojaInitCallbackCh <- 1
    }()
    gojaRequire.RegisterNativeModule("console", requireConsole)
}

func GenGojaExceptionString(vm *GojaVM, jserr *goja.Exception) string {
    var b bytes.Buffer
    b.WriteString(jserr.Value().String())
    b.WriteByte('\n')

    for i, stack := range jserr.Stacks() {
        b.WriteString("\tat ")
        source, _, line, column, ok := vm.consumer.Source(stack.Position().Line, stack.Position().Col)
        if ok {
            b.WriteString(source)
            b.WriteByte(':')
            b.WriteString(strconv.Itoa(line))
            b.WriteByte(':')
            b.WriteString(strconv.Itoa(column))
            b.WriteString(" (")
            b.WriteString(strconv.Itoa(i))
            b.WriteByte(')')
        } else {
            b.WriteString(stack.SrcName())
            b.WriteByte(':')
            b.WriteString(stack.Position().String())
        }
        b.WriteByte('\n')
    }

    return b.String()
}

func GenGojaStackFrameString(vm *GojaVM, title string, stacks []goja.StackFrame) string {
    var b bytes.Buffer
    b.WriteString(title)
    b.WriteByte('\n')

    var ii = 0
    for _, stack := range stacks {
        source, _, line, column, ok := vm.consumer.Source(stack.Position().Line, stack.Position().Col)
        if ok {
            b.WriteString("\tat ")
            b.WriteString(source)
            b.WriteByte(':')
            b.WriteString(strconv.Itoa(line))
            b.WriteByte(':')
            b.WriteString(strconv.Itoa(column))
            b.WriteString(" (")
            b.WriteString(strconv.Itoa(ii))
            b.WriteByte(')')
            b.WriteByte('\n')
            ii += 1
        } else {
            continue
            //b.WriteString(stack.SrcName())
            //b.WriteByte(':')
            //b.WriteString(stack.Position().String())
        }
    }

    return b.String()
}

func CreateGojaVM() *GojaVM {
    vm := new(GojaVM)
    vm.Runtime = goja.New()
    return vm
}

func (vm *GojaVM) Called() int64 { return 0 }
func (vm *GojaVM) Reset()        {}
func (vm *GojaVM) PrintMemStat() {}

func (vm *GojaVM) SetValue(name string, val interface{}) {
    if name == "CurrentSessionId" {
        s := uint64(codecs.Int64FromInterface(val))
        if s == 0 {
            if vm.sidForLock > 0 {
                globalStorage.Unlock(vm.sidForLock, vm.defKeyForLock)
            }
            if vm.defKeyForRedis > 0 {
                globalStorage.RedisClose(vm.defKeyForRedis)
            }
        }
        vm.sidForLock = 0
        vm.defKeyForLock = s
        vm.defKeyForRedis = 0
    }
    vm.Runtime.Set(name, vm.Runtime.ToValue(val))
}

func (vm *GojaVM) Dispose() {
}

func (vm *GojaVM) Load(path string) bool {
    fbs, err := ioutil.ReadFile(path)
    if err != nil {
        return false
    }

    vm.consumer = nil
    fmapbs, err := ioutil.ReadFile(path + ".map")
    if err == nil {
        wd, _ := os.Getwd()
        sUrl, _ := url.Parse(wd + "/")
        fUrl, _ := url.Parse(path)
        rUrl := sUrl.ResolveReference(fUrl)
        vm.consumer, err = sourcemap.Parse("file://" + rUrl.String(), fmapbs)
    }

    gojaRequire.Enable(vm.Runtime)
    EnableConsole(vm.Runtime)

    gn := &GojaVMNet{vm: vm}
    obj := vm.Runtime.NewObject()
    obj.Set("sendCurrentPlayer", gn.SendCurrentPlayer)
    obj.Set("sendToOtherPlayer", gn.SendToOtherPlayer)
    vm.Runtime.Set("net", obj)

    objLock := vm.Runtime.NewObject()
    objLock.Set("init", gn.InitLock)
    objLock.Set("dispose", gn.DisposeLock)
    objLock.Set("lock", gn.Lock)
    objLock.Set("unlock", gn.Unlock)
    vm.Runtime.Set("sync", objLock)

    objDB := vm.Runtime.NewObject()
    objDB.Set("query", gn.Query)
    objDB.Set("exec", gn.Exec)
    objDB.Set("transaction", gn.Transaction)
    vm.Runtime.Set("mysql", objDB)

    objRedis := vm.Runtime.NewObject()
    objRedis.Set("open", gn.Open)
    objRedis.Set("close", gn.Close)
    objRedis.Set("doCommand", gn.Do)
    objRedis.Set("send", gn.Send)
    objRedis.Set("flush", gn.Flush)
    objRedis.Set("receive", gn.Receive)
    vm.Runtime.Set("redis", objRedis)

    _, err = vm.Runtime.RunScript(path, string(fbs))
    if err != nil {
        if jserr, ok := err.(*goja.Exception); ok {
            utils.LogError("[J] %s", GenGojaExceptionString(vm, jserr))
        }
        return false
    }

    isRunInited := false
    gojaInit := vm.Runtime.Get("__init__")
    if gojaInit == nil || goja.IsUndefined(gojaInit) {
        utils.LogError("[J] 脚本上下文中缺少了初始化入口函数__init__")
        return false
    } else {
        init, ok := goja.AssertFunction(gojaInit)
        if ok {
            _, ok := <-gojaInitCallbackCh
            if ok {
                r, err := init(goja.Undefined())
                if err != nil {
                    if jserr, ok := err.(*goja.Exception); ok {
                        utils.LogError("[J] %s", GenGojaExceptionString(vm, jserr))
                    }
                }
                isRunInited = true
                close(gojaInitCallbackCh)
                if r == nil || goja.IsUndefined(r) || goja.IsNull(r) {
                    return false
                }
                if r.ToInteger() != 0 {
                    return false
                }
            }
        } else {
            utils.LogError("[J] 脚本上下文中缺少了初始化入口函数__init__")
            return false
        }
    }

    gojaMain := vm.Runtime.Get("__main__")
    if gojaMain == nil || goja.IsUndefined(gojaMain) {
        //...
    } else {
        main, ok := goja.AssertFunction(gojaMain)
        if ok {
            _, err := main(goja.Undefined())
            if isRunInited && err != nil {
                if jserr, ok := err.(*goja.Exception); ok {
                    utils.LogError("[J] %s", GenGojaExceptionString(vm, jserr))
                }
            }
        }
    }

    return true
}

func (vm *GojaVM) SetAssociatedSourceAddr(addr string) {
    vm.associatedSourceAddr = addr
}

func (vm *GojaVM) SetAssociatedSourceId(id uint64) {
    vm.associatedSourceId = id
}

func (vm *GojaVM) GetAssociatedSourceAddr() string {
    return vm.associatedSourceAddr
}

func (vm *GojaVM) GetAssociatedSourceId() uint64 {
    return vm.associatedSourceId
}

func (vm *GojaVM) DispatchEnter(sessionId uint64, addr string) int {
    gojaEnter := vm.Runtime.Get("__enter__")
    if gojaEnter == nil || goja.IsUndefined(gojaEnter) {
        return -1
    }
    enter, ok := goja.AssertFunction(gojaEnter)
    if ok {
        _, err := enter(goja.Undefined(), vm.Runtime.ToValue(sessionId), vm.Runtime.ToValue(addr))
        if err != nil {
            if jserr, ok := err.(*goja.Exception); ok {
                utils.LogError("[J] %s", GenGojaExceptionString(vm, jserr))
            }
        }
    }
    return 0
}

func (vm *GojaVM) DispatchLeave(sessionId uint64, addr string) int {
    gojaEnter := vm.Runtime.Get("__leave__")
    if gojaEnter == nil || goja.IsUndefined(gojaEnter) {
        return -1
    }
    enter, ok := goja.AssertFunction(gojaEnter)
    if ok {
        _, err := enter(goja.Undefined(), vm.Runtime.ToValue(sessionId), vm.Runtime.ToValue(addr))
        if err != nil {
            if jserr, ok := err.(*goja.Exception); ok {
                utils.LogError("[J] %s", GenGojaExceptionString(vm, jserr))
            }
        }
    }
    return 0
}

func transferGoArray2GojaArray(goArray []interface{}) []interface{} {
    newArray := make([]interface{}, len(goArray))
    for i, v := range goArray {
        switch v.(type) {
        case map[interface{}]interface{}:
            newArray[i] = transferGoMap2GojaMap(v.(map[interface{}]interface{}))
        case []interface{}:
            newArray[i] = transferGoArray2GojaArray(v.([]interface{}))
        default:
            newArray[i] = v
        }
    }
    return newArray
}

func transferGoMap2GojaMap(goMap map[interface{}]interface{}) map[string]interface{} {
    out := make(map[string]interface{})
    for k, v := range goMap {
        sk := ""
        switch k.(type) {
        case int:
            sk = strconv.FormatInt(int64(k.(int)), 10)
        case int8:
            sk = strconv.FormatInt(int64(k.(int8)), 10)
        case int16:
            sk = strconv.FormatInt(int64(k.(int16)), 10)
        case int32:
            sk = strconv.FormatInt(int64(k.(int32)), 10)
        case int64:
            sk = strconv.FormatInt(k.(int64), 10)
        case uint:
            sk = strconv.FormatUint(uint64(k.(uint)), 10)
        case uint8:
            sk = strconv.FormatUint(uint64(k.(uint8)), 10)
        case uint16:
            sk = strconv.FormatUint(uint64(k.(uint16)), 10)
        case uint32:
            sk = strconv.FormatUint(uint64(k.(uint32)), 10)
        case uint64:
            sk = strconv.FormatUint(k.(uint64), 10)
        case string:
            sk = k.(string)
        default:
            continue
        }

        rv := v
        switch rv.(type) {
        case map[interface{}]interface{}:
            rv = transferGoMap2GojaMap(v.(map[interface{}]interface{}))
        case []interface{}:
            rv = transferGoArray2GojaArray(v.([]interface{}))
        }

        out[sk] = rv
    }
    return out
}

func (vm *GojaVM) DispatchMessage(sessionId uint64, msg map[interface{}]interface{}) int {
    gojaEnter := vm.Runtime.Get("__message__")
    if gojaEnter == nil || goja.IsUndefined(gojaEnter) {
        return -1
    }
    message, ok := goja.AssertFunction(gojaEnter)
    if ok {
        _, err := message(goja.Undefined(), vm.Runtime.ToValue(sessionId), vm.Runtime.ToValue(transferGoMap2GojaMap(msg)))
        if err != nil {
            if jserr, ok := err.(*goja.Exception); ok {
                utils.LogError("[J] %s", GenGojaExceptionString(vm, jserr))
            }
        }
    }
    return 0
}
