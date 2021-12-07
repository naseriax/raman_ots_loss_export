package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type nxPm struct {
	NoOfEntries int `json:"noOfEntries"`
	StartIndex  int `json:"startIndex"`
}

func getFiberChar(agent RestAgent, ots map[string]interface{}) []map[string]interface{} {
	log.Printf("Retrieving the Fiber Characteristics for OTS: %v\n", fmt.Sprintf("%v", ots["guiLabel"]))
	resp := agent.HttpGet(fmt.Sprintf("/data/npr/physicalConns/%v/fiberCharacteristic", fmt.Sprintf("%v", ots["id"])), map[string]string{})
	_, fiberCharInfo := GeneralJsonDecoder(resp)
	return fiberCharInfo
}

func GetRamanConnections(agent RestAgent, ldType string) []map[string]interface{} {
	rawOtsData := agent.HttpGet("/data/npr/physicalConns", map[string]string{})
	_, listJson := GeneralJsonDecoder(rawOtsData)
	var otslist []map[string]interface{}
	for _, phycon := range listJson {
		if phycon["wdmConnectionType"] == "WdmPortType_ots" {
			if strings.Contains(fmt.Sprintf("%v", phycon["aPortLabel"]), ldType) || strings.Contains(fmt.Sprintf("%v", phycon["zPortLabel"]), ldType) || strings.Contains(fmt.Sprintf("%v", phycon["a2PortLabel"]), ldType) || strings.Contains(fmt.Sprintf("%v", phycon["z2PortLabel"]), ldType) {
				otslist = append(otslist, phycon)
			}
		}
	}
	return otslist
}

func nxPmConList(agent RestAgent) []map[string]interface{} {
	url := "/mncpm/mdcxnlist/"
	payload := &nxPm{
		NoOfEntries: 10000,
		StartIndex:  0,
	}
	nxPmRawData := agent.HttpPostJson(url, payload, map[string]string{})
	_, nxPmJsonData := GeneralJsonDecoder(nxPmRawData)
	return nxPmJsonData
}

func timeCalculator() [2]int {
	ts := time.Now().Unix()
	return [2]int{int(ts), int(ts) - 3600}
}

func getPortPower(agent RestAgent, portInfo []string, otsPmId string) (map[string]interface{}, bool) {
	log.Printf("Retrieving the RX Power on %v\n", portInfo[0])
	log.Printf("Retrieving the RX Power on %v\n", portInfo[1])
	ts := timeCalculator()
	payload := map[string]interface{}{
		"objIds":       []string{otsPmId},
		"endTime":      ts[0],
		"fileType":     "CSV",
		"granularity":  "15mins",
		"startTime":    ts[1],
		"passwd":       "",
		"sftp":         "inactive",
		"username":     "",
		"fileLocation": "",
	}
	rawPmData := agent.HttpPostJson("/mncpm/connection/query", payload, map[string]string{})
	jsonPmData, _ := GeneralJsonDecoder(rawPmData)
	l1, ok := jsonPmData["objGraphDataMap"].([]interface{})[0].(map[string]interface{})["graphDataMap"].(map[string]interface{})["OPIN/TOPR-AVG (Receive/NEND)"]
	if !ok {
		log.Printf("error - PM Data not found for %v - %v", portInfo[0], portInfo[1])
		return nil, ok
	}
	pmData := l1.(map[string]interface{})["pmdata"].([]interface{})
	pmInfo := map[string]interface{}{}
	for _, port := range portInfo {
		t := map[string]string{}
		for _, data_if := range pmData {
			data := data_if.(map[string]interface{})
			if val, ok := data[port]; ok {
				if val == "" {
					continue
				} else {
					if len(t) == 0 {
						t[port] = fmt.Sprintf("%v", data[port])
						t["Time"] = fmt.Sprintf("%v", data["Time"])
					} else {
						layout := "01/02/2006 15:04"
						t1, _ := time.Parse(layout, t["Time"])
						t2, _ := time.Parse(layout, fmt.Sprintf("%v", data["Time"]))
						if t1.Unix() < t2.Unix() {
							t[port] = fmt.Sprintf("%v", data[port])
							t["Time"] = fmt.Sprintf("%v", data["Time"])
						}
					}
				}
			} else if val, ok := data[port+"(Z End)"]; ok {
				if val == "" {
					continue
				} else {
					if len(t) == 0 {
						t[port] = fmt.Sprintf("%v", data[port+"(Z End)"])
						t["Time"] = fmt.Sprintf("%v", data["Time"])
					} else {
						layout := "01/02/2006 15:04"
						t1, _ := time.Parse(layout, t["Time"])
						t2, _ := time.Parse(layout, fmt.Sprintf("%v", data["Time"]))
						if t1.Unix() < t2.Unix() {
							t[port] = fmt.Sprintf("%v", data[port+"(Z End)"])
							t["Time"] = fmt.Sprintf("%v", data["Time"])
						}
					}
				}
			}
		}
		val, err := strconv.ParseFloat(t[port], 64)
		errDealer(err)
		pmInfo[port] = val
		pmInfo["Time"] = t["Time"]
	}
	log.Printf("Used the PM date at: %v UTC\n", pmInfo["Time"])
	fmt.Println("#################################################################################")
	return pmInfo, ok
}

func exportFile(output [][]string) error {
	ts := fmt.Sprintf("%v", timeCalculator()[0])
	csvFile, err := os.Create(fmt.Sprintf("output_%v.csv", ts))
	if err != nil {
		return err
	} else {
		csvwriter := csv.NewWriter(csvFile)
		for _, empRow := range output {
			_ = csvwriter.Write(empRow)
		}
		csvwriter.Flush()
	}
	return nil
}

func main() {
	var ipaddr, uname, passw, ldType string
	flag.StringVar(&uname, "u", "admin", "Specify the username. Default is admin.")
	flag.StringVar(&passw, "p", "***", "Specify the password.")
	flag.StringVar(&ipaddr, "i", "127.0.0.1", "Specify NFM-T IP Address. Default is 127.0.0.1.")
	flag.StringVar(&ldType, "l", "RA2P", "Specify The LD type. Default is RA2P.")
	flag.Usage = func() {
		fmt.Printf("Usage: \n")
		fmt.Printf("./otsloss.exe -n admin -p password -i 192.168.0.1 -l RA2P \n")
	}
	flag.Parse()

	var wg1 sync.WaitGroup
	var wg2 sync.WaitGroup

	restAgent := Init(ipaddr, uname, passw)
	defer restAgent.NfmtDeauth()

	otsList := GetRamanConnections(restAgent, ldType)

	nxPm := nxPmConList(restAgent)
	masterfile := []map[string]interface{}{}
	for _, ots := range otsList {
		wg2.Add(1)
		go func(ots map[string]interface{}) {
			var otsPmId map[string]interface{}
			for _, item := range nxPm {
				if item["cxnName"] == ots["guiLabel"] {
					otsPmId = item
				}
			}
			chars := getFiberChar(restAgent, ots)
			cores := []map[string]interface{}{}
			pmData, ok := getPortPower(restAgent, []string{fmt.Sprintf("%v", ots["z2PortLabel"]), fmt.Sprintf("%v", ots["zPortLabel"])}, fmt.Sprintf("%v", otsPmId["cxnId"]))
			if !ok {
				wg2.Done()
				return
			}
			fmt.Println("Returned!!!!!!!!!1")
			for _, core := range chars {
				wg1.Add(1)
				go func(core map[string]interface{}) {
					corePowerData := map[string]interface{}{"egressPort": core["fromLabel"], "ingressPort": core["toLabel"], "egressPower": core["egressPowerOut"], "ingressPower": pmData[fmt.Sprintf("%v", core["toLabel"])], "ramanGain": core["targetGainStr"], "totalLoss": 0}
					egPower, err := strconv.ParseFloat(fmt.Sprintf("%v", corePowerData["egressPower"]), 64)
					errDealer(err)
					inPower, err := strconv.ParseFloat(fmt.Sprintf("%v", corePowerData["ingressPower"]), 64)
					errDealer(err)
					if corePowerData["ramanGain"] != "N.A." {
						ramanGain, err := strconv.ParseFloat(fmt.Sprintf("%v", corePowerData["ramanGain"]), 64)
						errDealer(err)
						corePowerData["totalLoss"] = ((egPower - inPower + ramanGain) * 1000) / 1000
					} else {
						corePowerData["totalLoss"] = ((egPower - inPower) * 1000) / 1000
					}
					cores = append(cores, corePowerData)
					wg1.Done()
				}(core)
			}
			wg1.Wait()
			masterfile = append(masterfile, map[string]interface{}{"ots": ots, "cores": cores})
			wg2.Done()
		}(ots)
	}
	wg2.Wait()
	output := [][]string{{"OTS Name", "Egress Port", "Egress Power", "Ingress Port", "Ingress Power", "Raman Gain", "Total Loss"}}
	for _, res := range masterfile {
		index := fmt.Sprintf("%v", res["ots"].(map[string]interface{})["guiLabel"])
		for _, co := range res["cores"].([]map[string]interface{}) {
			rowToAdd := []string{index, fmt.Sprintf("%v", co["egressPort"]), fmt.Sprintf("%v", co["egressPower"]), fmt.Sprintf("%v", co["ingressPort"]), fmt.Sprintf("%v", co["ingressPower"]), fmt.Sprintf("%v", co["ramanGain"]), fmt.Sprintf("%v", co["totalLoss"])}
			output = append(output, rowToAdd)
		}
	}
	if len(output) == 1 {
		restAgent.NfmtDeauth()
		log.Fatalf("error - no PM Data has been collected!")
	} else {
		err := exportFile(output)
		if err == nil {
			fmt.Println("SUCCESS: Loss report file has been exported!")
		} else {
			panic(err)
		}
	}

}
