package main

import (
    "sync/atomic"

    "github.com/packing/clove/codecs"
    "github.com/packing/clove/errors"
    "github.com/packing/clove/messages"
)

type ClientMessageObject struct {
}

func OnDeliver(msg *messages.Message) error {
    data := msg.GetBody()
    if data == nil {
        return errors.ErrorDataIsDamage
    }

    realMsg, err := messages.MessageFromData(nil, "", data)
    if err != nil || realMsg == nil {
        return errors.ErrorDataNotMatch
    }

    vm := getVM()
    if vm != nil {
        vm.SetValue("CurrentSessionId", realMsg.GetSessionId()[0])
        vm.SetAssociatedSessionId(realMsg.GetSessionId()[0])

        aids := msg.GetSessionId()
        if aids != nil && len(aids) > 0 {
            vm.SetAssociatedSourceId(aids[0])
        } else {
            vm.SetAssociatedSourceId(0)
            vm.SetAssociatedSourceAddr(msg.GetUnixSource())
        }

        if realMsg.GetType() == messages.ProtocolTypeClientEnter {
            addr := ""
            body := realMsg.GetBody()
            if body != nil {
                r := codecs.CreateMapReader(body)
                addr = r.StrValueOf(messages.ProtocolKeyHost, addr)
            }
            //初始化默认全局锁键
            globalStorage.InitLock(realMsg.GetSessionId()[0])
            vm.DispatchEnter(realMsg.GetSessionId()[0], addr)
        } else if realMsg.GetType() == messages.ProtocolTypeClientLeave {
            addr := ""
            body := realMsg.GetBody()
            if body != nil {
                r := codecs.CreateMapReader(body)
                addr = r.StrValueOf(messages.ProtocolKeyHost, addr)
            }
            //销毁全局锁键
            globalStorage.DisposeLock(realMsg.GetSessionId()[0])
            vm.DispatchLeave(realMsg.GetSessionId()[0], addr)
        } else {
            vm.DispatchMessage(realMsg.GetSessionId()[0], data)
        }

        vm.SetValue("CurrentSessionId", 0)
        freeVM(vm)
    }

    //utils.LogError("errorCode >>>", msg.GetErrorCode())
    if msg.GetErrorCode() == 0 {

    } else {
        clientSessionIds, ok := data[messages.ProtocolKeySessionId]
        if ok {
            ackMsg := make(codecs.IMMap)
            ackMsg[messages.ProtocolKeyTag] = codecs.IMSlice{messages.ProtocolTagAdapter}
            ackMsg[messages.ProtocolKeyScheme] = messages.ProtocolSchemeS2S
            ackMsg[messages.ProtocolKeyType] = messages.ProtocolTypeFlowReturn
            ackMsg[messages.ProtocolKeySessionId] = clientSessionIds

            ssids := msg.GetSessionId()
            if ssids != nil && len(ssids) > 0 {
                ackMsg[messages.ProtocolKeySerial] = ssids[0]
                tcpCtrl.Send(ackMsg)
            } else {
                unix.SendTo(msg.GetUnixSource(), ackMsg)
                atomic.AddUint64(&unlockflow, 1)
            }
        }
    }

    return nil
}

func (receiver ClientMessageObject) GetMappedTypes() map[int]messages.MessageProcFunc {
    msgMap := make(map[int]messages.MessageProcFunc)
    msgMap[messages.ProtocolTypeDeliver] = OnDeliver
    return msgMap
}
