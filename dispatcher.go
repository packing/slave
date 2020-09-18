package main

import (
    "github.com/packing/nbpy/errors"
    "github.com/packing/nbpy/messages"
)

type ClientMessageObject struct {
}

func OnDeliver(msg *messages.Message) (error) {
    data := msg.GetBody()
    if data == nil {
        return errors.ErrorDataIsDamage
    }

    realMsg, err := messages.MessageFromData(nil, "", data)
    if err != nil || realMsg == nil {
        return errors.ErrorDataNotMatch
    }

    vm := getVM()
    ssids := msg.GetSessionId()
    if ssids != nil && len(ssids) > 0 {
        vm.SetAssociatedSourceId(ssids[0])
    } else {
        vm.SetAssociatedSourceId(0)
        vm.SetAssociatedSourceAddr(msg.GetUnixSource())
    }
    vm.DispatchMessage(realMsg.GetSessionId()[0], data)
    freeVM(vm)
    return nil
}

func (receiver ClientMessageObject) GetMappedTypes() (map[int]messages.MessageProcFunc) {
    msgMap := make(map[int]messages.MessageProcFunc)
    msgMap[messages.ProtocolTypeDeliver] = OnDeliver
    return msgMap
}
