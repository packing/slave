package main

import (
    "github.com/packing/v8go"
)

var freeVMQueue chan v8go.VM
var recoverVMQueue chan v8go.VM

func createQueue(limit int) bool {
    freeVMQueue = make(chan v8go.VM, cpuNum)
    if scriptEngine == ScriptEngineV8 {
        recoverVMQueue = make(chan v8go.VM, cpuNum)
    }
    for i := 0; i < limit; i ++ {
        var vm v8go.VM
        if scriptEngine == ScriptEngineV8 {
            //vm = v8go.CreateV8VM()
        } else if scriptEngine == ScriptEngineGoja {
            vm = CreateGojaVM()
        } else {
            return false
        }
        if !vm.Load(sckDir) {
            return false
        }
        go func() {
            freeVMQueue <- vm
        }()
    }
    return true
}

func getVM() v8go.VM {
    vm := <- freeVMQueue

    return vm
}

func freeVM(vm v8go.VM) {
    go func() {
        if scriptEngine == ScriptEngineV8 {
            recoverVMQueue <- vm
        } else if scriptEngine == ScriptEngineGoja {
            if freeVMQueue == nil {
                return
            }
            freeVMQueue <- vm
        }
    }()
}

func getVMFree() int {
    return len(freeVMQueue)
}

func purgeVM() {
    if scriptEngine == ScriptEngineV8 {
        for {
            vm, ok := <-recoverVMQueue
            if !ok {
                return
            }
            if vm.Called() > 1000 {
                vm.Reset()
                vm.Load(sckDir)
            }
            //fmt.Printf("清洗 %p\n", vm)

            freeVMQueue <- vm
        }
    }
}

func disposeQueue() {
    for getVMFree() > 0 {
        vm := getVM()
        if vm != nil {
            vm.Dispose()
        }
    }
    if scriptEngine == ScriptEngineV8 {
        close(recoverVMQueue)
    }
    close(freeVMQueue)
    freeVMQueue = nil
}