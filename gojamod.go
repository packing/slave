package main

import (
    "fmt"
    "nbpy/codecs"
    "nbpy/gojainner"
    "nbpy/net"
    "nbpy/utils"
    "strconv"
    "strings"

    "github.com/dop251/goja"
)

type EventListener struct {
    eventTag int64
    eventType int64
    modName string
    funcName string
}

var (
    GlobalGojaModules = make([]gojainner.ScriptModule, 1)
    gojaEventListeners = make(map[int64] EventListener)
    GlobalMessageMapping = make(map[int64] map[string]int)
    GlobalMessageReversed = make(map[int64] map[int]string)
    GlobalEventDispatcherProgram *goja.Program = nil
)

func toGojaScriptMessage(msg codecs.IMMap) map[string]interface{} {
    nMsg := make(map[string]interface{})
    for k, v := range msg {
        rk := strconv.FormatInt(codecs.Int64FromInterface(k), 10)
        if rk == "0" {
            continue
        }
        rv := v
        switch v.(type) {
        case map[interface{}]interface{}:
            rv = toGojaScriptMessage(v.(map[interface{}]interface{}))
        }
        nMsg[rk] = rv
    }
    return nMsg
}

func fromGojaScriptMessage(msg map[string]interface{}) codecs.IMMap{
    nMsg := make(codecs.IMMap)
    for k, v := range msg {
        rk, err := strconv.ParseInt(k, 0, 64)
        if err != nil {
            continue
        }
        rv := v
        switch v.(type) {
        case map[string]interface{}:
            rv = fromGojaScriptMessage(v.(map[string]interface{}))
        }
        nMsg[rk] = rv
    }
    return nMsg
}

type SysModule struct {
    runtime *goja.Runtime
}

func SysModuleLoader(runtime *goja.Runtime, module *goja.Object) {
    t := new(SysModule)
    t.runtime = runtime
    o := module.Get("exports").(*goja.Object)
    o.Set("bindEventListener", t.bindEventListener)
    o.Set("buildEventDispatch", t.buildEventDispatch)
    o.Set("registerMessage", t.registerMessage)
}

func (s *SysModule) bindEventListener(call goja.FunctionCall) goja.Value {
    if len(call.Arguments) != 3 {
        utils.LogError("绑定脚本事件监听器参数错误")
        return s.runtime.ToValue(false)
    }
    argv0 := call.Arguments[0].String()
    if argv0 == "" {
        utils.LogError("绑定脚本事件监听器参数modName错误, 必须是一个有效的名称")
        return s.runtime.ToValue(false)
    }
    argv2 := call.Arguments[1].ToInteger()
    if argv2 == 0 {
        utils.LogError("绑定脚本事件监听器参数messageType错误, 必须是不为0的有效数字")
        return s.runtime.ToValue(false)
    }
    argv3 := call.Arguments[2].String()
    if argv3 == "" {
        utils.LogError("绑定脚本事件监听器参数funcName错误, 必须是一个有效的名称")
        return s.runtime.ToValue(false)
    }

    gojaEventListeners[argv2] = EventListener{eventType: argv2, modName: argv0, funcName: argv3}
    return s.runtime.ToValue(true)
}

func (s *SysModule) buildEventDispatch(call goja.FunctionCall) goja.Value {

    caseCode := make([]string,0)

    for k, v := range gojaEventListeners {
        caseCode = append(caseCode, fmt.Sprintf(`case %d: return require("./scripts/events/%s.js").events.%s(tp, msg);`, k, v.modName, v.funcName))
    }

    code := fmt.Sprintf(`var messageIn = function(tp, msg) {
switch (tp) {
%s
default: return [0, nil];
}
}`, strings.Join(caseCode, "\n"))

    //utils.LogInfo("生成映射器代码 ->", code)

    var err error
    GlobalEventDispatcherProgram, err = goja.Compile("", code, false)
    if err == nil {
        return s.runtime.ToValue(true)
    } else {
        return s.runtime.ToValue(false)
    }
}

func (s *SysModule) registerMessage(call goja.FunctionCall) goja.Value {
    utils.LogInfo("registerMessage ->", call.Arguments)
    if len(call.Arguments) != 1 {
        utils.LogError("绑定脚本消息类型参数错误")
        return s.runtime.ToValue(false)
    }
    argv := call.Arguments[0]
    if goja.IsUndefined(argv) || goja.IsNull(argv) {
        utils.LogError("绑定脚本消息类型参数messageInst错误, 必须是一个有效的消息对象实例")
        return s.runtime.ToValue(false)
    }
    argvO := argv.ToObject(s.runtime)
    if argvO == nil {
        utils.LogError("绑定脚本消息类型参数messageInst错误, 必须是一个有效的消息对象实例")
        return s.runtime.ToValue(false)
    }
    jtp := argvO.Get("messageType")
    if jtp == nil || goja.IsUndefined(jtp) || goja.IsNull(jtp) {
        utils.LogError("绑定脚本消息类型参数messageInst错误, 必须是一个有效的消息对象实例")
        return s.runtime.ToValue(false)
    }

    jmapping := argvO.Get("__mapping__")
    if jmapping == nil || goja.IsUndefined(jmapping) || goja.IsNull(jmapping) {
        utils.LogError("绑定脚本消息类型参数messageInst错误, 必须是一个有效的消息对象实例")
        return s.runtime.ToValue(false)
    }

    jreversed := argvO.Get("__reversed__")
    if jreversed == nil || goja.IsUndefined(jreversed) || goja.IsNull(jreversed) {
        utils.LogError("绑定脚本消息类型参数messageInst错误, 必须是一个有效的消息对象实例")
        return s.runtime.ToValue(false)
    }

    GlobalMessageMapping[jtp.ToInteger()] = jmapping.Export().(map[string]int)
    GlobalMessageReversed[jtp.ToInteger()] = jreversed.Export().(map[int]string)
    return s.runtime.ToValue(true)
}

func onGojaEnter(id net.SessionID, addr string) {
    vm, fn, ok := gojaHost.GetCallable("onEnter")
    if ok {
        fn(goja.Undefined(), vm.ToValue(id), vm.ToValue(addr))
    }
}

func onGojaLeave(id net.SessionID, addr string) {
    vm, fn, ok := gojaHost.GetCallable("onLeave")
    if ok {
        fn(goja.Undefined(), vm.ToValue(id), vm.ToValue(addr))
    }
}

func onGojaMessage(messageType int64, messageData map[string] interface{}) (int64, map[string]interface{}) {
    /*vm, fn, ok := gojaHost.GetCallable("onMessage")
    if ok {
        r, err := fn(goja.Undefined(), vm.ToValue(messageType), vm.ToValue(messageData))
        if err == nil {
            if !goja.IsUndefined(r) && !goja.IsNull(r) {
                gor := r.Export()
                ret, ok := gor.([]interface{})
                if ok && len(ret) == 2 {
                    code, ok := ret[0].(int64)
                    if !ok {
                        code = 0
                    }
                    body, ok := ret[1].(map[string]interface{})
                    if ok {
                        return code, body
                    }
                }
            }
        }
    }*/

    if GlobalEventDispatcherProgram == nil {
        return 0, nil
    }

    vm, cb, ok := gojaHost.GetCallableWithProgram(GlobalEventDispatcherProgram, "messageIn")
    if ok {
        r, err := cb(goja.Undefined(), vm.ToValue(messageType), vm.ToValue(messageData))
        if err == nil {
            if !goja.IsUndefined(r) && !goja.IsNull(r) {
                gor := r.Export()
                ret, ok := gor.([]interface{})
                if ok && len(ret) == 2 {
                    code, ok := ret[0].(int64)
                    if !ok {
                        code = 0
                    }
                    body, ok := ret[1].(map[string]interface{})
                    if ok {
                        return code, body
                    }
                }
            }
        } else {
            utils.LogError("消息处理脚本出错->", err)
        }
    }

    return 0, nil
}

