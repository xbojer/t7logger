package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/roffe/gocan"
	"github.com/roffe/gocan/adapter"
	"github.com/roffe/t7logger/pkg/kwp2000"
)

var vars []*kwp2000.VarDefinition

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile | log.Lmicroseconds)

	//if b, err := json.MarshalIndent(vars, "", "  "); err == nil {
	//	log.Println(string(b))
	//}

	b, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatal(err)
	}
	if err := json.Unmarshal(b, &vars); err != nil {
		log.Fatal(err)
	}

}

var freq = 50

/*
var vars = []*kwp2000.VarDefinition{
	{
		Name:   "ActualIn.n_Engine", // 2
		Method: kwp2000.VAR_METHOD_SYMBOL,
		Value:  3428,
		Type:   kwp2000.TYPE_WORD,
		Signed: true,
	},
	{
		Name:   "ActualIn.T_AirInlet", // 2
		Method: kwp2000.VAR_METHOD_SYMBOL,
		Value:  3436,
		Type:   kwp2000.TYPE_WORD,
		Signed: true,
	},
	{
		Name:   "ActualIn.p_AirInlet", // 2
		Method: kwp2000.VAR_METHOD_SYMBOL,
		Value:  3438,
		Type:   kwp2000.TYPE_WORD,
		Signed: true,
	},
	{
		Name:   "ActualIn.T_Engine", // 2
		Method: kwp2000.VAR_METHOD_SYMBOL,
		Value:  3435,
		Type:   kwp2000.TYPE_WORD,
		Signed: true,
	},
	{
		Name:   "IgnProt.fi_Offset", // 2
		Method: kwp2000.VAR_METHOD_SYMBOL,
		Value:  3012,
		Type:   kwp2000.TYPE_WORD,
		Signed: true,
	},
	{
		Name:   "ActualIn.p_AirAmbient", // 2
		Method: kwp2000.VAR_METHOD_SYMBOL,
		Value:  3437,
		Type:   kwp2000.TYPE_WORD,
		Signed: true,
		//Unit:     "kPa",
		Forumula: "%v/10.0",
	},
	{
		Name:   "ActualIn.ST_IgnitionKey", // 1
		Method: kwp2000.VAR_METHOD_SYMBOL,
		Value:  3457,
		Type:   kwp2000.TYPE_BYTE,
	},
	{
		Name:   "AdpFuelAdap.AddFuelAdapt", // 4
		Method: kwp2000.VAR_METHOD_SYMBOL,
		Value:  2091,
		Type:   kwp2000.TYPE_LONG,
		Signed: false,
		//Unit:   "mg/c",
	},
	{
		Name:   "ActualIn.U_Batt", // 2
		Method: kwp2000.VAR_METHOD_SYMBOL,
		Value:  3456,
		Type:   kwp2000.TYPE_WORD,
		Signed: false,
		//Unit:   "V",
	},
	//	{
	//		Name:   "ActualIn.Q_AirInlet",
	//		Method: kwp2000.VAR_METHOD_LOCID,
	//		Value:  104,
	//		Type:   kwp2000.TYPE_WORD,
	//		Signed: true,
	//	},
	//	{
	//		Name:   "MAF.m_AirInlet",
	//		Method: kwp2000.VAR_METHOD_LOCID,
	//		Value:  105,
	//		Type:   kwp2000.TYPE_WORD,
	//		Signed: false,
	//	},
}
*/

func main() {
	quitChan := make(chan os.Signal, 2)
	signal.Notify(quitChan, os.Interrupt, syscall.SIGTERM)

	// Open the log file for writing
	file, err := os.OpenFile("mylog.t7l", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	var devName string
	for _, d := range adapter.List() {
		if strings.HasPrefix(d, "CANUSB") {
			devName = d
		}
	}

	dev, err := adapter.New(
		devName,
		&gocan.AdapterConfig{
			//Port:         `C:\Program Files (x86)\Drew Technologies, Inc\J2534\MongoosePro GM II\monpa432.dll`,
			Port:         "COM7",
			PortBaudrate: 3000000,
			CANRate:      500,
			CANFilter:    []uint32{0x238, 0x258, 0x270},
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	c, err := gocan.New(ctx, dev)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	k := kwp2000.New(c)
	if err := k.StartSession(ctx, kwp2000.INIT_MSG_ID, kwp2000.INIT_RESP_ID); err != nil {
		log.Println(err)
		return
	}

	log.Println("Defining DynamicallyDefineLocalId's...")
	for i, v := range vars {
		log.Printf("%d %s %s %d %X", i, v.Name, v.Method, v.Value, v.Type)
		if err := k.DynamicallyDefineLocalIdRequest(ctx, i, v); err != nil {
			log.Println(err)
			return
		}
	}

	count := 0

	log.Printf("Starting live logging at %d fps", freq)

	print(strings.Repeat("\n", len(vars)))

	t := time.NewTicker(time.Second / time.Duration(freq))
	defer t.Stop()
	for {
		select {
		case <-quitChan:
			log.Println("Exiting...")
			if err := k.StopSession(ctx, kwp2000.INIT_MSG_ID); err != nil {
				log.Println(err)
			}
			time.Sleep(250 * time.Millisecond)
			return
		case <-t.C:
			data, err := k.ReadDataByLocalIdentifier(ctx, 0xF0)
			if err != nil {
				log.Println(err)
				continue
			}
			r := bytes.NewReader(data)

			print(strings.Repeat("\033[A", len(vars)))
			for _, va := range vars {
				if err := va.Read(r); err != nil {
					log.Println(err)
				}
				log.Println(va.String())
			}

			if r.Len() > 0 {
				left := r.Len()
				leftovers := make([]byte, r.Len())
				n, err := r.Read(leftovers)
				if err != nil {
					log.Println(err)
				}
				log.Printf("leftovers %d: %X", left, leftovers[:n])
			}

			fmt.Printf("Frames captured: %d\n\033[A", count)

			produceLogLine(file, vars)

			count++
		}
	}

}

var out strings.Builder

func produceLogLine(file io.Writer, vars []*kwp2000.VarDefinition) {
	out.WriteString("|")
	for _, va := range vars {
		out.WriteString(va.String() + "|")
	}
	fmt.Fprint(file, time.Now().Format("02-01-2006 15:04:05.999")+out.String()+"IMPORTANTLINE=0|\n")
	out.Reset()
}
