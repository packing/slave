package main

import (
    "bytes"
    "io/ioutil"
    "net/url"
    "os"
    "strconv"

    "github.com/go-sourcemap/sourcemap"
    "github.com/packing/goja"
    "github.com/packing/goja_nodejs/require"
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

type GojaVM struct {
    Runtime              *goja.Runtime
    associatedSourceAddr string
    associatedSourceId   uint64
    consumer             *sourcemap.Consumer
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
        println(stack.Position().String())
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

func CreateGojaVM() *GojaVM {
    vm := new(GojaVM)
    vm.Runtime = goja.New()
    return vm
}

func (vm *GojaVM) Called() int64 { return 0 }
func (vm *GojaVM) Reset()        {}
func (vm *GojaVM) PrintMemStat() {}

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
    _, err = vm.Runtime.RunScript(path, string(fbs))
    if err != nil {
        if jserr, ok := err.(*goja.Exception); ok {
            utils.LogError("[J] %s", GenGojaExceptionString(vm, jserr))
        }
        return false
    }
    gojaRequire.Enable(vm.Runtime)
    EnableConsole(vm.Runtime)

    gn := &GojaVMNet{vm: vm}
    obj := vm.Runtime.NewObject()
    obj.Set("sendCurrentPlayer", gn.SendCurrentPlayer)
    obj.Set("sendToOtherPlayer", gn.SendToOtherPlayer)
    vm.Runtime.Set("net", obj)

    isRunInited := false
    gojaInit := vm.Runtime.Get("init")
    if gojaInit == nil || goja.IsUndefined(gojaInit) {
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
            return false
        }
    }

    gojaMain := vm.Runtime.Get("main")
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
    gojaEnter := vm.Runtime.Get("enter")
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
    gojaEnter := vm.Runtime.Get("leave")
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
    gojaEnter := vm.Runtime.Get("message")
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
