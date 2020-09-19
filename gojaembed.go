package main

import (
    "io/ioutil"
    "strconv"

    "github.com/dop251/goja"
)

type GojaVM struct {
    runtime *goja.Runtime
    associatedSourceAddr string
    associatedSourceId uint64
}


func CreateGojaVM() *GojaVM {
    vm := new(GojaVM)
    vm.runtime = goja.New()
    return vm
}

func (vm *GojaVM) Dispose() {
}

func (vm *GojaVM) Load(path string) bool {
    fbs, err := ioutil.ReadFile(path)
    if err != nil {
        return false
    }
    _, err = vm.runtime.RunScript(path, string(fbs))
    if  err != nil {
        return false
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
    gojaEnter := vm.runtime.Get("enter")
    if gojaEnter == nil || goja.IsUndefined(gojaEnter) {
        return -1
    }
    enter, ok := goja.AssertFunction(gojaEnter)
    if ok {
        enter(goja.Undefined(), vm.runtime.ToValue(sessionId), vm.runtime.ToValue(addr))
    }
    return 0
}

func (vm *GojaVM) DispatchLeave(sessionId uint64, addr string) int {
    gojaEnter := vm.runtime.Get("leave")
    if gojaEnter == nil || goja.IsUndefined(gojaEnter) {
        return -1
    }
    enter, ok := goja.AssertFunction(gojaEnter)
    if ok {
        enter(goja.Undefined(), vm.runtime.ToValue(sessionId), vm.runtime.ToValue(addr))
    }
    return 0
}

func transferGoArray2GojaArray(goArray []interface{}) []interface{} {
    newArray := make([]interface{}, len(goArray))
    for i, v := range goArray {
        switch v.(type) {
        case map[interface{}] interface{}: newArray[i] = transferGoMap2GojaMap(v.(map[interface{}] interface{}))
        case []interface{}: newArray[i] = transferGoArray2GojaArray(v.([]interface{}))
        default:
            newArray[i] = v
        }  
    }
    return newArray
}

func transferGoMap2GojaMap(goMap map[interface{}] interface{}) map[string] interface{} {
    out := make(map[string] interface{})
    for k,v := range goMap {
        sk := ""
        switch k.(type) {
        case int: sk = strconv.FormatInt(int64(k.(int)), 10)
        case int8: sk = strconv.FormatInt(int64(k.(int8)), 10)
        case int16: sk = strconv.FormatInt(int64(k.(int16)), 10)
        case int32: sk = strconv.FormatInt(int64(k.(int32)), 10)
        case int64: sk = strconv.FormatInt(k.(int64), 10)
        case uint: sk = strconv.FormatUint(uint64(k.(uint)), 10)
        case uint8: sk = strconv.FormatUint(uint64(k.(uint8)), 10)
        case uint16: sk = strconv.FormatUint(uint64(k.(uint16)), 10)
        case uint32: sk = strconv.FormatUint(uint64(k.(uint32)), 10)
        case uint64: sk = strconv.FormatUint(k.(uint64), 10)
        case string: sk = k.(string)
        default:
            continue
        }

        rv := v
        switch rv.(type) {
        case map[interface{}] interface{}: rv = transferGoMap2GojaMap(v.(map[interface{}] interface{}))
        case []interface{}: rv = transferGoArray2GojaArray(v.([]interface{}))
        }
        
        out[sk] = rv
    }
    return out
}

func (vm *GojaVM) DispatchMessage(sessionId uint64, msg map[interface{}] interface{}) int {
    gojaEnter := vm.runtime.Get("message")
    if gojaEnter == nil || goja.IsUndefined(gojaEnter) {
        return -1
    }
    message, ok := goja.AssertFunction(gojaEnter)
    if ok {
        message(goja.Undefined(), vm.runtime.ToValue(sessionId), vm.runtime.ToValue(transferGoMap2GojaMap(msg)))
    }
    return 0
}