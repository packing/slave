package main

import (
	"flag"
	"fmt"
	"log"
	"nbpy/codecs"
	"nbpy/env"
    "nbpy/gojainner"
    "nbpy/messages"
	"nbpy/net"
	"nbpy/packets"
    "nbpy/tengoinner"
    "nbpy/utils"
	"os"
	"runtime"
	"runtime/pprof"
	"syscall"
    "time"
)

const (
    ScriptEngineTengo = iota
    ScriptEngineGoja
)

var (
	help    bool
	version bool

	daemon bool
    setsided bool

	addr   string
	dbAddr string
	adapterAddr string

	pprofFile string

	logDir   string
	logLevel = utils.LogLevelVerbose
	pidFile  string

	sckDir = "./scripts"

	cpuNum  = 0

	scriptEngine = ScriptEngineTengo

	tengoHost *tengoinner.ScriptHost
    gojaHost *gojainner.ScriptHost

	unix     *net.UnixUDP = nil
	unixSend *net.UnixUDP = nil
)

func usage() {
	fmt.Fprint(os.Stderr, `slave

Usage: slave [-hv] [-d daemon] [-f pprof file] [-b db addr] [-m cpu limit] [-e script engine]

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
    req[messages.ProtocolKeyGoroutine] = runtime.NumGoroutine()
    req[messages.ProtocolKeyValue] = 0
	msg.SetBody(req)
	pck, err := messages.DataFromMessage(msg)
	if err == nil {
        _, err = unixSend.SendTo(addr, pck)
    } else {

    }
	return err
}

func main() {

	flag.BoolVar(&help, "h", false, "help message")
	flag.BoolVar(&version, "v", false, "print version")
	flag.BoolVar(&daemon, "d", false, "run at daemon")
    flag.BoolVar(&setsided, "s", false, "already run at daemon")
	flag.StringVar(&pprofFile, "f", "", "pprof file")
	flag.StringVar(&dbAddr, "b", "", "db addr")
    flag.IntVar(&cpuNum, "m", 0, "cpu limit")
    flag.IntVar(&scriptEngine, "e", ScriptEngineTengo, "script engine")
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

	if cpuNum > 0 {
	    runtime.GOMAXPROCS(cpuNum)
    }

	addr = "./sockets/master.sock"
    adapterAddr = "./sockets/adapter.sock"
	logDir = "./logs/slave"
    if !daemon {
        logDir = ""
    } else {
        if !setsided {
            utils.Daemon()
            return
        }
    }

    //os.Chdir("../")

	pidFile = "./pid"
	utils.GeneratePID(pidFile)

	unixAddr := "./sockets/slave.sock"
	unixSendAddr := "./sockets/slave_sender.sock"

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

        syscall.Unlink(unixSendAddr)
        syscall.Unlink(unixAddr)

		utils.RemovePID(pidFile)

		utils.LogInfo(">>> 进程已退出")
	}()

	utils.LogInit(logLevel, logDir)

	//注册解码器
	env.RegisterCodec(codecs.CodecIMv2)

	//注册通信协议
	env.RegisterPacketFormat(packets.PacketFormatNB)

	//创建s2s管道
	_, err := os.Stat(unixAddr)
	if err == nil || !os.IsNotExist(err) {
		err = os.Remove(unixAddr)
		if err != nil {
			utils.LogError("无法删除unix管道旧文件", err)
		}
	}
	_, err = os.Stat(unixSendAddr)
	if err == nil || !os.IsNotExist(err) {
		err = os.Remove(unixSendAddr)
		if err != nil {
			utils.LogError("无法删除unix发送管道旧文件", err)
		}
	}

	if scriptEngine == ScriptEngineTengo {
        //脚本引擎初始化
        GlobalTengoModules[0] = tengoinner.ScriptModule{Name:"net", Module: netTengoModule}
        GlobalTengoModules[1] = tengoinner.ScriptModule{Name:"sys", Module: sysTengoModule}

        tengoHost, err = tengoinner.CreateScriptHost(sckDir, GlobalTengoModules...)
    } else if scriptEngine == ScriptEngineGoja {
        GlobalGojaModules[0] = gojainner.ScriptModule{Name:"sys", Loader:SysModuleLoader}
        gojaHost, err = gojainner.CreateScriptHost(sckDir, GlobalGojaModules...)
        if err != nil {
            utils.LogError("!!!Goja脚本初始化出错", err)
            return
        }
        gojaHost.OnInitialize()
    } else {
        utils.LogError("!!!不支持的脚本引擎类型 %d", scriptEngine)
        return
    }
    if err != nil {
        utils.LogError("!!!脚本引擎初始化失败", err)
    }

    messages.GlobalDispatcher.MessageObjectMapped(messages.ProtocolSchemeS2S, messages.ProtocolTagSlave, ClientMessageObject{})
    messages.GlobalDispatcher.Dispatch()

    unix = net.CreateUnixUDPWithFormat(packets.PacketFormatNB, codecs.CodecIMv2)
	unix.OnDataDecoded = messages.GlobalMessageQueue.Push
	err = unix.Bind(unixAddr)
	if err != nil {
		utils.LogError("!!!无法创建unix管道", unixAddr, err)
		unix.Close()
		return
	}
	unixSend = net.CreateUnixUDPWithFormat(packets.PacketFormatNB, codecs.CodecIMv2)
	err = unixSend.Bind(unixSendAddr)
	if err != nil {
		utils.LogError("!!!无法创建unix发送管道", unixSendAddr, err)
        unix.Close()
		unixSend.Close()
		return
	}

    go func() {
        for {
            if !daemon {
                agvt, tmax, tmin := messages.GlobalDispatcher.GetAsyncInfo()
                fmt.Printf(">>> 当前 事务 = [平均: %.2f, 峰值: %.2f | %.2f] 编码 = [编码: %.2f, 解码: %.2f] 网络 = [TCP读: %d, TCP写: %d, UNIX读: %d, UNIX写: %d]\r",
                    float64(agvt)/float64(time.Millisecond), float64(tmin)/float64(time.Millisecond), float64(tmax)/float64(time.Millisecond),
                    float64(net.GetEncodeAgvTime())/float64(time.Millisecond), float64(net.GetDecodeAgvTime())/float64(time.Millisecond),
                    net.GetTotalTcpRecvSize(),
                    net.GetTotalTcpSendSize(),
                    net.GetTotalUnixRecvSize(),
                    net.GetTotalUnixSendSize())
            }
            runtime.Gosched()
            time.Sleep(1 * time.Second)
        }
    }()

    go func() {
        for {
            sayHello()
            runtime.Gosched()
            time.Sleep(10 * time.Second)
        }
    }()

    utils.LogInfo(">>> 当前协程数量 > %d", runtime.NumGoroutine())
    env.Schedule()

    if scriptEngine == ScriptEngineTengo {
    } else if scriptEngine == ScriptEngineGoja {
        gojaHost.OnDestory()
    }
    unixSend.Close()
	unix.Close()
}
