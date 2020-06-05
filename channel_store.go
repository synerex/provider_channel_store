package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/golang/protobuf/ptypes"

	pb "github.com/synerex/synerex_api"
	sxutil "github.com/synerex/synerex_sxutil"
)

var (
	nodesrv         = flag.String("nodesrv", "127.0.0.1:9990", "Node ID Server")
	channel         = flag.Int("channel", 3, "Recording channel type")
	dir             = flag.String("dir", "store", "Directory of data storage")     // for all file
	saveFile        = flag.String("saveFile", "", "Save to single file with name") //
	mu              sync.Mutex
	sxServerAddress string
	msgCount        int64
	ds              DataStore
)

// DataStore is a interface for storing strings.
type DataStore interface {
	store(str string)
}

// FileSystemDataStore stores data into file
type FileSystemDataStore struct {
	storeDir  string
	storeFile *os.File
	todayStr  string
}

func init() {
	var err error
	msgCount = 0
	dataDir, err := os.Getwd()
	if err != nil {
		fmt.Printf("Can't obtain current wd")
	}
	dataDir = filepath.ToSlash(dataDir) + "/" + *dir
	ds = &FileSystemDataStore{
		storeDir: dataDir,
	}
}

// open file with today info
func (fs *FileSystemDataStore) store(str string) {
	const layout = "2006-01-02"
	day := time.Now()
	todayStr := day.Format(layout) + ".csv"
	if len(*saveFile) == 0 {
		if fs.todayStr != "" && fs.todayStr != todayStr {
			fs.storeFile.Close()
			fs.storeFile = nil
		}
		if fs.storeFile == nil {
			_, er := os.Stat(fs.storeDir)
			if er != nil { // create dir
				er = os.MkdirAll(fs.storeDir, 0777)
				if er != nil {
					fmt.Printf("Can't make dir '%s'.", fs.storeDir)
					return
				}
			}
			fs.todayStr = todayStr
			file, err := os.OpenFile(filepath.FromSlash(fs.storeDir+"/"+todayStr), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
			if err != nil {
				fmt.Printf("Can't open file '%s'", todayStr)
				return
			}
			fs.storeFile = file
		}
	} else {
		if fs.storeFile == nil {
			file, err := os.OpenFile(*saveFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
			if err != nil {
				fmt.Printf("Can't open file '%s'", *saveFile)
				return
			}
			fs.storeFile = file
		}
	}
	fs.storeFile.WriteString(str + "\n")
}

// callback for each Supply
func supplyCallback(clt *sxutil.SXServiceClient, sm *pb.Supply) {
	msgCount++
	// we need to store sm into csv file.
	ts := ptypes.TimestampString(sm.Ts)
	bsd := base64.StdEncoding.EncodeToString(sm.Cdata.Entity)
	line := fmt.Sprintf("%s,%d,%d,%d,%d,%s,%s,%d,%s", ts, sm.Id, sm.SenderId, sm.TargetId, sm.ChannelType, sm.SupplyName, sm.ArgJson, sm.MbusId, bsd)
	ds.store(line)
}

func subscribeSupply(client *sxutil.SXServiceClient) {
	// goroutine!
	ctx := context.Background() //
	for {
		client.SubscribeSupply(ctx, supplyCallback)
		// comes here if channel closed
		log.Printf("Server closed... on Forward provider")

		time.Sleep(5 * time.Second)
		newClt := sxutil.GrpcConnectServer(sxServerAddress)
		if newClt != nil {
			log.Printf("Reconnect server [%s]", sxServerAddress)
			client.Client = newClt
		}
	}
}

// just for stat
func monitorStatus() {
	for {
		sxutil.SetNodeStatus(int32(msgCount), fmt.Sprintf("recv:%d", msgCount))
		time.Sleep(time.Second * 3)
	}
}

func main() {
	log.Printf("ChannelStore(%s) built %s sha1 %s", sxutil.GitVer, sxutil.BuildTime, sxutil.Sha1Ver)
	flag.Parse()

	go sxutil.HandleSigInt()
	sxutil.RegisterDeferFunction(sxutil.UnRegisterNode)

	channelTypes := []uint32{uint32(*channel)}
	// obtain synerex server address from nodeserv
	srcSSrv, err := sxutil.RegisterNode(*nodesrv, fmt.Sprintf("ChannelStore[%d]", *channel), channelTypes, nil)
	if err != nil {
		log.Fatal("Can't register to nodeserv...")
	}
	log.Printf("Connecting Server [%s]\n", srcSSrv)
	sxServerAddress = srcSSrv

	wg := sync.WaitGroup{} // for syncing other goroutines
	srcClient := sxutil.GrpcConnectServer(sxServerAddress)
	argJson := fmt.Sprintf("{ChannelStore[%d]}", *channel)
	sxClient := sxutil.NewSXServiceClient(srcClient, uint32(*channel), argJson)

	wg.Add(1)

	// currently only work for supply ....
	// ToDO: add demand store.
	go subscribeSupply(sxClient)
	go monitorStatus()

	wg.Wait()
	sxutil.CallDeferFunctions() // cleanup!

}
