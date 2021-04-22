package main

import (
    "flag"
    "fmt"
    "log"
    "os"
    "runtime"
    "runtime/pprof"
    "syscall"
    "time"

    "github.com/packing/clove/codecs"
    "github.com/packing/clove/env"
    "github.com/packing/clove/messages"
    "github.com/packing/clove/nnet"
    "github.com/packing/clove/packets"
    "github.com/packing/clove/storage"
    "github.com/packing/clove/utils"

    //"github.com/packing/v8go"
)

const (
    ScriptEngineV8 = iota
    ScriptEngineGoja
)

var (
    help    bool
    version bool

    daemon   bool

    onlyTCP  bool

    unlockflow uint64

    locklogic uint64
    unlocklogic uint64

    addr        string
    unixAddr    string
    addrStorage string

    pprofFile string

    logDir   string
    logLevel = utils.LogLevelVerbose
    pidFile  string

    sckDir = "./app.js"

    cpuNum = 0

    scriptEngine = ScriptEngineGoja

    unix            *nnet.UnixUDP = nil
    tcpCtrl         *nnet.TCPClient = nil
    globalStorage   *storage.Client = nil
)

func usage() {
    fmt.Fprint(os.Stderr, `slave

Usage: slave [-hv] [-d daemon] [-f pprof file] [-c master addr] [-m vm limit] [-e script entryfile]

Options:
`)
    flag.PrintDefaults()
}



func sayHello() error {
    defer func() {
        utils.LogPanic(recover())
    }()
    msg := messages.CreateS2SMessage(messages.ProtocolTypeSlaveHello)
    msg.SetTag(messages.ProtocolTagMaster)
    req := codecs.IMMap{}
    req[messages.ProtocolKeyId] = os.Getpid()
    if !onlyTCP {
        req[messages.ProtocolKeyUnixAddr] = unixAddr
    }
    req[messages.ProtocolKeyValue] = getVMFree()
    msg.SetBody(req)
    pck, err := messages.DataFromMessage(msg)
    if err == nil {
        tcpCtrl.Send(pck)
    }
    return err
}

func reportState() error {
    defer func() {
        utils.LogPanic(recover())
    }()
    msg := messages.CreateS2SMessage(messages.ProtocolTypeSlaveChange)
    msg.SetTag(messages.ProtocolTagMaster)
    req := codecs.IMMap{}
    req[messages.ProtocolKeyValue] = getVMFree()
    msg.SetBody(req)
    pck, err := messages.DataFromMessage(msg)
    if err == nil {
        tcpCtrl.Send(pck)
    }
    return err
}

func sendMessage(sAddr string, sId uint64, message codecs.IMData) int {
    body, ok := message.(codecs.IMMap)
    if !ok {
        return 0
    }
    useUnixSocket := sId == 0
    msg := messages.CreateS2SMessage(messages.ProtocolTypeDeliver)
    msg.SetTag(messages.ProtocolTagAdapter)
    msg.SetBody(body)
    if !useUnixSocket {
        //msg.SetTag(messages.ProtocolTagMaster)
        if sId > 0 {
            msg.SetSessionId([]nnet.SessionID{sId})
        }
    }
    pck, err := messages.DataFromMessage(msg)
    if err == nil {
        if useUnixSocket {
            unix.SendTo(sAddr, pck)
        } else {
            tcpCtrl.Send(pck)
        }
    }
    return 0
}

func sendMessageTo(message codecs.IMData) int {
    body, ok := message.(codecs.IMMap)
    if !ok {
        return 0
    }

    msg := messages.CreateS2SMessage(messages.ProtocolTypeDeliver)
    msg.SetTag(messages.ProtocolTagAdapter)
    msg.SetBody(body)
    pck, err := messages.DataFromMessage(msg)
    if err == nil {
        tcpCtrl.Send(pck)
    }
    return 0
}

func sendSysMessage(message codecs.IMData) int {
    tcpCtrl.Send(message)
    //utils.LogError("sendSysMessage >>>", message)
    return 0
}

func main() {

    //runtime.GOMAXPROCS(2)

    flag.BoolVar(&help, "h", false, "help message")
    flag.BoolVar(&version, "v", false, "print version")
    flag.BoolVar(&daemon, "d", false, "run at daemon")
    flag.BoolVar(&onlyTCP, "t", false, "only tcp tunnel")
    flag.StringVar(&pprofFile, "f", "", "pprof file")
    flag.StringVar(&addr, "c", "127.0.0.1:10088", "controller addr")
    flag.StringVar(&addrStorage, "b", "/tmp/storage.sock", "storage addr")
    flag.IntVar(&cpuNum, "m", 100, "cpu limit")
    flag.StringVar(&sckDir, "e", sckDir, "script entryfile")
    flag.Usage = usage

    flag.Parse()
    if help {
        flag.Usage()
        syscall.Exit(-1)
        return
    }
    if version {
        fmt.Println("slave version 1.0")
        syscall.Exit(-1)
        return
    }

    logDir = "./logs/slave"
    if !daemon {
        logDir = ""
    } else {
        if os.Getppid() != 1 {
            utils.Daemon()
            return
        }
    }

    pidFile = "./pid"
    utils.GeneratePID(pidFile)

    unixAddr = fmt.Sprintf("/tmp/slave_%d.sock", os.Getpid())

    var pproff *os.File = nil
    if pprofFile != "" {
        pf, err := os.OpenFile(pprofFile, os.O_RDWR|os.O_CREATE, 0644)
        if err != nil {
            log.Fatal(err)
        }
        pproff = pf
        pprof.StartCPUProfile(pproff)
    }

    defer func() {
        if pproff != nil {
            pprof.StopCPUProfile()
            pproff.Close()
        }

        syscall.Unlink(unixAddr)

        utils.RemovePID(pidFile)

        utils.LogInfo(">>> 进程已退出")
    }()

    utils.LogInit(logLevel, logDir)

    //注册解码器
    env.RegisterCodec(codecs.CodecIMv2)

    //注册通信协议
    env.RegisterPacketFormat(packets.PacketFormatNB)

    //清理sock文件
    _, err := os.Stat(unixAddr)
    if err == nil || !os.IsNotExist(err) {
        err = os.Remove(unixAddr)
        if err != nil {
            utils.LogError("无法删除unix管道旧文件", err)
        }
    }

    globalStorage = storage.CreateClient(addrStorage, time.Second * 5)

    if scriptEngine == ScriptEngineV8 {
        /*utils.LogInfo("==============================================================")
        utils.LogInfo(">>> 当前V8引擎版本: %s", v8go.Version())
        utils.LogInfo(">>> 上下文缓冲数量: %d", cpuNum)
        utils.LogInfo("==============================================================")
        v8go.Init()

        v8go.OnOutput = func(s string) {
            utils.LogRaw(s)
        }

        v8go.OnSendMessage = sendMessage
        v8go.OnSendMessageTo = sendMessageTo*/

    } else if scriptEngine == ScriptEngineGoja {
        GojaInit()

        OnGojaSendMessage = sendMessage
        OnGojaSendMessageTo = sendMessageTo
        OnGojaSendSysMessage = sendSysMessage
    } else {
        utils.LogError("!!!不支持的脚本引擎类型 %d", scriptEngine)
        return
    }

    if cpuNum > 0 {
        if !createQueue(cpuNum) {
            cpuNum = 0
            utils.LogError("!!! 脚本池初始化失败")
            return
        }
    }

    messages.GlobalDispatcher.MessageObjectMapped(messages.ProtocolSchemeS2S, messages.ProtocolTagSlave, ClientMessageObject{})
    messages.GlobalDispatcher.Dispatch()

    //初始化unixsocket发送管道
    unix = nnet.CreateUnixUDPWithFormat(packets.PacketFormatNB, codecs.CodecIMv2)
    unix.OnDataDecoded = messages.GlobalMessageQueue.Push
    err = unix.Bind(unixAddr)
    if err != nil {
        utils.LogError("!!!无法创建unixsocket管道 => %s", unixAddr, err)
        unix.Close()
        return
    }

    tcpCtrl = nnet.CreateTCPClient(packets.PacketFormatNB, codecs.CodecIMv2)
    tcpCtrl.OnDataDecoded = messages.GlobalMessageQueue.Push
    err = tcpCtrl.Connect(addr, 0)
    if err != nil {
        utils.LogError("!!!无法连接到控制服务器 %s", addr, err)
        unix.Close()
        tcpCtrl.Close()
        return
    } else {
        sayHello()
    }

    go func() {
        for {
            if !daemon {
                agvt, tmax, tmin := messages.GlobalDispatcher.GetAsyncInfo()
                fmt.Printf(">>> 当前 事务 = [平均: %.2f, 峰值: %.2f | %.2f] VM = [FREE: %d, USED: %d] 网络 = [TCP读: %d, TCP写: %d, UNIX读: %d, UNIX写: %d, 流解锁: %d, 逻辑锁: %d / %d]\r",
                    float64(agvt)/float64(time.Millisecond), float64(tmin)/float64(time.Millisecond), float64(tmax)/float64(time.Millisecond),
                    getVMFree(), cpuNum - getVMFree(),
                    nnet.GetTotalTcpRecvSize(),
                    nnet.GetTotalTcpSendSize(),
                    nnet.GetTotalUnixRecvSize(),
                    nnet.GetTotalUnixSendSize(),
                        unlockflow, locklogic, unlocklogic)
            }
            runtime.Gosched()
            time.Sleep(1 * time.Second)
        }
    }()
    go func() {
        for {
            time.Sleep(10 * time.Second)
            reportState()
            runtime.Gosched()
        }
    }()

    go purgeVM()

    utils.LogInfo(">>> 当前协程数量 > %d", runtime.NumGoroutine())
    env.Schedule()

    disposeQueue()

    if scriptEngine == ScriptEngineV8 {
        //v8go.Dispose()
    }

    if globalStorage != nil {
        globalStorage.Close()
    }
    tcpCtrl.Close()
    unix.Close()
}
