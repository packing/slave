package main

import (
    "nbpy/codecs"
    "nbpy/errors"
    "nbpy/messages"
    "nbpy/utils"
)

type ClientMessageObject struct {
}

func OnDeliver(msg *messages.Message) (error) {
    data := msg.GetBody()
    if data == nil {
        return errors.ErrorDataIsDamage
    }

    realMsg, err := messages.MessageFromData(nil, "", data)
    if err != nil {
        return errors.ErrorDataNotMatch
    }

    tp := realMsg.GetType()

    if scriptEngine == ScriptEngineTengo {

        comp, ok := eventListeners[int64(tp)]
        if !ok {
            utils.LogInfo("消息 %d 未绑定脚本处理器", tp)
        }

        wrapData := toTengoScriptMessage(data)
        ret, err := tengoHost.OnMessageIn(comp, wrapData)
        if err != nil {
            utils.LogError("消息 %d 脚本处理器执行出错", tp, err)
        }
        if ret != nil && len(ret) == 2 {
            errcode := ret[0].(int64)
            body := ret[1].(map[string]interface{})

            msgbody := fromTengoScriptMessage(body)

            retMsg := messages.CreateS2CMessage(int(tp))
            retMsg.SetErrorCode(int(errcode))
            retMsg.SetBody(msgbody)

            retData, err := messages.DataFromMessage(retMsg)

            if err == nil {
                deliver := messages.CreateS2SMessage(messages.ProtocolTypeDeliver)
                deliver.SetTag(messages.ProtocolTagAdapter)
                deliver.SetSessionId(realMsg.GetSessionId())
                deliver.SetBody(retData.(codecs.IMMap))

                sentData, err := messages.DataFromMessage(deliver)
                if err == nil {
                    unixSend.SendTo(adapterAddr, sentData)
                }
            }
        }
    } else if scriptEngine == ScriptEngineGoja {
        wrapData := toGojaScriptMessage(data)
        code ,retBody := onGojaMessage(int64(tp), wrapData)
        if retBody != nil {

            msgBody := fromGojaScriptMessage(retBody)

            retMsg := messages.CreateS2CMessage(int(tp))
            retMsg.SetErrorCode(int(code))
            retMsg.SetBody(msgBody)

            retData, err := messages.DataFromMessage(retMsg)

            if err == nil {
                deliver := messages.CreateS2SMessage(messages.ProtocolTypeDeliver)
                deliver.SetTag(messages.ProtocolTagAdapter)
                deliver.SetSessionId(realMsg.GetSessionId())
                deliver.SetBody(retData.(codecs.IMMap))

                sentData, err := messages.DataFromMessage(deliver)
                if err == nil {
                    unixSend.SendTo(adapterAddr, sentData)
                }
            }
        }
    }
    return nil
}

func (receiver ClientMessageObject) GetMappedTypes() (map[int]messages.MessageProcFunc) {
    msgMap := make(map[int]messages.MessageProcFunc)
    msgMap[messages.ProtocolTypeDeliver] = OnDeliver
    return msgMap
}