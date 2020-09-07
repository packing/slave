package main

import (
    "fmt"
    "io/ioutil"
    "nbpy/codecs"
    "nbpy/messages"
    "nbpy/tengoinner"
    "nbpy/utils"
    "strconv"
    "strings"

    "nbpy/errors"

    "script/tengo"

)

var (
    GlobalTengoModules = make([]tengoinner.ScriptModule, 2)

    referenceKeys = make(map[string]int64)
    referenceKeyReverseds = make(map[int64]string)


    processingModule = ""
    eventListeners = make(map[int64] *tengo.Compiled)
)

func toTengoScriptMessage(msg codecs.IMMap) map[string]interface{} {
    nMsg := make(map[string]interface{})
    for k, v := range msg {
        rk := codecs.Int64FromInterface(k)
        sk, ok := referenceKeyReverseds[rk]
        if !ok {
            sk = strconv.FormatInt(rk, 10)
        }
        rv := v
        switch v.(type) {
        case map[interface{}]interface{}:
            rv = toTengoScriptMessage(v.(codecs.IMMap))
        }
        nMsg[sk] = rv
    }
    return nMsg
}

func fromTengoScriptMessage(msg map[string]interface{}) codecs.IMMap{
    nMsg := make(codecs.IMMap)
    for k, v := range msg {
        ik, ok := referenceKeys[k]
        if !ok {
            sik, err := strconv.Atoi(k)
            if err != nil {
                continue
            }
            ik = int64(sik)
        }
        rv := v
        switch v.(type) {
        case map[string]interface{}:
            rv = fromTengoScriptMessage(v.(map[string]interface{}))
        }
        nMsg[ik] = rv
    }
    return nMsg
}

var sysTengoModule = map[string]tengo.Object{
    "addReference":   &tengo.UserFunction{Name: "addReference", Value: tengoAddReference},
    "findModules":   &tengo.UserFunction{Name: "findModules", Value: tengofindModules},
    "bindEventListener": &tengo.UserFunction{Name: "bindEventListener", Value: tengobindEventListener},
}

func tengobindEventListener(args ...tengo.Object) (ret tengo.Object, err error) {

    numArgs := len(args)
    if numArgs != 2 {
        return nil, tengo.ErrWrongNumArguments
    }

    nType, ok := args[0].(*tengo.Int)
    if !ok {
        return nil, tengo.ErrInvalidArgumentType{
            Name:     "type",
            Expected: "Int",
            Found:    args[0].TypeName(),
        }
    }
    callName, ok := args[1].(*tengo.String)
    if !ok {
        return nil, tengo.ErrInvalidArgumentType{
            Name:     "callName",
            Expected: "String",
            Found:    args[1].TypeName(),
        }
    }

    code := fmt.Sprintf(`mod := import("events/%s");result = mod.%s(argv)`, processingModule, callName.Value)
    comp, err := tengoinner.CreateCompile(code, sckDir, GlobalTengoModules...)
    if err == nil {
        eventListeners[nType.Value] = comp
        utils.LogInfo("绑定脚本消息事件处理器 %d => %s.%s", nType.Value, processingModule, callName.Value)
    }

    return nil, nil
}

func tengofindModules(args ...tengo.Object) (ret tengo.Object, err error) {

    numArgs := len(args)
    if numArgs != 1 {
        return nil, tengo.ErrWrongNumArguments
    }

    sv, ok := args[0].(*tengo.String)
    if !ok {
        return nil, tengo.ErrInvalidArgumentType{
            Name:     "dir",
            Expected: "String",
            Found:    args[0].TypeName(),
        }
    }
    fs, err := ioutil.ReadDir(sv.Value)
    flist := make([]interface{}, 0)
    if err == nil {
        for _, f := range fs {
            if f.IsDir() || !strings.HasSuffix(f.Name(), ".tengo") {
                continue
            }
            fn := strings.TrimSuffix(f.Name(), ".tengo")
            processingModule = fn
            code := fmt.Sprintf(`mod := import("events/%s");mod.__bind__()`, fn)
            err = tengoinner.RunScript(code, sckDir, GlobalTengoModules...)
            if err != nil {
                utils.LogInfo("初始化消息事件模块 %s 出错.", err)
            }

        }
    }
    ro, err := tengo.FromInterface(flist)
    if err == nil {
        return ro, nil
    }
    return nil, nil
}

func tengoAddReference(args ...tengo.Object) (ret tengo.Object, err error) {

    numArgs := len(args)
    if numArgs != 1 {
        return nil, tengo.ErrWrongNumArguments
    }

    mapv, ok := args[0].(*tengo.Map)
    if !ok {
        return nil, tengo.ErrInvalidArgumentType{
            Name:     "map",
            Expected: "Map",
            Found:    args[0].TypeName(),
        }
    }
    imap := tengo.ToInterface(mapv)
    if imap == nil {
        return nil, tengo.ErrInvalidArgumentType{
            Name:     "map",
            Expected: "Map",
            Found:    args[0].TypeName(),
        }
    }

    rmap, _ := imap.(map[string]interface{})

    immap := make(map[string]int64)
    for k, v := range rmap {
        _, exist := referenceKeys[k]
        if exist {
            return nil, errors.New(fmt.Sprintf("参照键名 %s 已经存在", k))
        }
        rv, ok := v.(int64)
        if !ok {
            continue
        }
        referenceKeys[k] = rv
        immap[k] = rv
    }

    for k, v := range immap {
        sk, exist := referenceKeyReverseds[v]
        if exist {
            return nil, errors.New(fmt.Sprintf("参照键 %s 的值已经被另一个参照键 %s 使用", k, sk))
        }
        referenceKeyReverseds[v] = k
    }


    return nil, nil
}

var netTengoModule = map[string]tengo.Object{
    "send":   &tengo.UserFunction{Name: "send", Value: tengoSend},
    "test":   &tengo.UserFunction{Name: "test", Value: tengoTest},
}

func tengoTest(_ ...tengo.Object) (ret tengo.Object, err error) {

    return nil, nil
}

func tengoSend(args ...tengo.Object) (ret tengo.Object, err error) {
    numArgs := len(args)
    if numArgs == 0 {
        return nil, tengo.ErrWrongNumArguments
    }

    msgv, ok := args[0].(*tengo.Map)
    if !ok {
        return nil, tengo.ErrInvalidArgumentType{
            Name:     "msg",
            Expected: "Map",
            Found:    args[0].TypeName(),
        }
    }

    iMsg := tengo.ToInterface(msgv)
    sMsg := iMsg.(map[string]interface{})
    toMsg := fromTengoScriptMessage(sMsg)

    reader := codecs.CreateMapReader(toMsg)

    scheme := reader.IntValueOf(messages.ProtocolKeyScheme, -1)
    if scheme < 0 {
        return nil, errors.New("message's scheme is unset yet")
    }

    if scheme == messages.ProtocolSchemeS2S {
        tag := reader.IntValueOf(messages.ProtocolKeyTag, -1)
        if tag < 0 {
            return nil, errors.New("message's tag is unset yet")
        }
        if tag == messages.ProtocolTagAdapter {
            unixSend.SendTo(adapterAddr, toMsg)
        } else if tag == messages.ProtocolTagMaster {
            unixSend.SendTo(addr, toMsg)
        } else {
            return nil, errors.New(fmt.Sprintf("message's tag %d is not be supported.", tag))
        }
    } else if scheme == messages.ProtocolSchemeS2C {
        iSessionId := reader.TryReadValue(messages.ProtocolKeySessionId)
        if iSessionId == nil {
            return nil, errors.New("message's sessionid is unset yet")
        }

        msg := messages.CreateS2SMessage(messages.ProtocolTypeDeliver)
        msg.SetTag(messages.ProtocolTagAdapter)
        msg.SetBody(toMsg)
        data, err := messages.DataFromMessage(msg)
        if err == nil {
            mapData, ok := data.(codecs.IMMap)
            if ok {
                mapData[messages.ProtocolKeySessionId] = iSessionId
                unixSend.SendTo(adapterAddr, mapData)
            }
        }
    } else {
        return nil, errors.New(fmt.Sprintf("message's scheme %d is not be supported.", scheme))
    }

    return nil, err
}
