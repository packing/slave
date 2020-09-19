package main

import (

    "github.com/packing/v8go"
)

var freeVMQueue chan *v8go.VM

func fillAllVM(limit int) bool {
    for i := 0; i < limit; i ++ {
        vm := v8go.CreateVM()
        if !vm.Load(sckDir) {
            return false
        }
        go func() {
            freeVMQueue <- vm
        }()
    }
    return true
}

func getVM() *v8go.VM {
    vm := <- freeVMQueue
    return vm
}

func freeVM(vm *v8go.VM) {
    go func() {
        freeVMQueue <- vm
    }()
}

func getVMFree() int {
    return len(freeVMQueue)
}

func freeAllVM() {
    for getVMFree() > 0 {
        vm := getVM()
        vm.PrintMemStat()
        vm.Dispose()
    }
    close(freeVMQueue)
}