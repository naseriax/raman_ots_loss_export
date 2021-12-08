package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type fiberCore struct {
	FromLabel      string `json:"fromLabel"`
	ToLabel        string `json:"toLabel"`
	EgressPowerOut string `json:"egressPowerOut"`
	RamanGain      string `json:"targetGainStr"`
}

type otsCon struct {
	APortLabel        string `json:"aPortLabel"`
	ZPortLabel        string `json:"zPortLabel"`
	A2PortLabel       string `json:"a2PortLabel"`
	Z2PortLabel       string `json:"z2PortLabel"`
	WdmConnectionType string `json:"wdmConnectionType"`
	GuiLabel          string `json:"guiLabel"`
	Id                int    `json:"id"`
}

type pmStruct struct {
	ObjGraphDataMap []struct {
		GraphDataMap struct {
			OPINTOPRAVGReceiveNEND struct {
				Pmdata []map[string]interface{} `json:"pmdata"`
			} `json:"OPIN/TOPR-AVG (Receive/NEND)"`
		} `json:"graphDataMap"`
	} `json:"objGraphDataMap"`
}

func GetRamanConnections(agent RestAgent, ldType string) []otsCon {
	rawOtsData := agent.HttpGet("/data/npr/physicalConns", map[string]string{})
	var listJson []otsCon
	json.Unmarshal([]byte(rawOtsData), &listJson)
	var otslist []otsCon
	for _, phycon := range listJson {
		if phycon.WdmConnectionType == "WdmPortType_ots" {
			if strings.Contains(fmt.Sprintf("%v", phycon.APortLabel), ldType) || strings.Contains(fmt.Sprintf("%v", phycon.ZPortLabel), ldType) || strings.Contains(fmt.Sprintf("%v", phycon.A2PortLabel), ldType) || strings.Contains(fmt.Sprintf("%v", phycon.Z2PortLabel), ldType) {
				otslist = append(otslist, phycon)
			}
		}
	}
	return otslist
}

func getFiberChar(agent RestAgent, ots otsCon) []fiberCore {
	log.Printf("Retrieving the Fiber Characteristics for OTS: %v\n", fmt.Sprintf("%v", ots.GuiLabel))
	resp := agent.HttpGet(fmt.Sprintf("/data/npr/physicalConns/%v/fiberCharacteristic", fmt.Sprintf("%v", ots.Id)), map[string]string{})
	var fiberCharInfo []fiberCore
	json.Unmarshal([]byte(resp), &fiberCharInfo)
	return fiberCharInfo
}

func nxPmConList(agent RestAgent) []map[string]interface{} {
	url := "/mncpm/mdcxnlist/"
	payload := map[string]int{
		"noOfEntries": 10000,
		"startIndex":  0,
	}
	nxPmRawData := agent.HttpPostJson(url, payload, map[string]string{})
	var nxPmJsonData []map[string]interface{}
	json.Unmarshal([]byte(nxPmRawData), &nxPmJsonData)
	return nxPmJsonData
}

func timeCalculator() [2]int {
	ts := time.Now().Unix()
	return [2]int{int(ts), int(ts) - 3600}
}

func getPortPower(agent RestAgent, portInfo []string, otsPmId string) map[string]interface{} {
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
	var jsonPmData pmStruct
	json.Unmarshal([]byte(rawPmData), &jsonPmData)
	pmData := jsonPmData.ObjGraphDataMap[0].GraphDataMap.OPINTOPRAVGReceiveNEND.Pmdata
	if len(pmData) == 0 {
		log.Printf("error - PM Data not found for %v - %v", portInfo[0], portInfo[1])
		return nil
	}
	pmInfo := map[string]interface{}{}
	for _, port := range portInfo {
		t := map[string]string{}
		for _, data := range pmData {
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
	return pmInfo
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

	masterfile := []map[string]interface{}{}

	restAgent := Init(ipaddr, uname, passw)
	defer restAgent.NfmtDeauth()

	otsList := GetRamanConnections(restAgent, ldType)

	nxPm := nxPmConList(restAgent)

	for _, ots := range otsList {
		wg2.Add(1)
		go func(ots otsCon) {
			var otsPmId map[string]interface{}
			for _, item := range nxPm {
				if item["cxnName"] == ots.GuiLabel {
					otsPmId = item
				}
			}
			chars := getFiberChar(restAgent, ots)
			cores := []map[string]interface{}{}
			pmData := getPortPower(restAgent, []string{fmt.Sprintf("%v", ots.Z2PortLabel), fmt.Sprintf("%v", ots.ZPortLabel)}, fmt.Sprintf("%v", otsPmId["cxnId"]))
			if pmData == nil {
				wg2.Done()
				return
			}
			for _, core := range chars {
				wg1.Add(1)
				go func(core fiberCore) {
					corePowerData := map[string]interface{}{"egressPort": core.FromLabel, "ingressPort": core.ToLabel, "egressPower": core.EgressPowerOut, "ingressPower": pmData[fmt.Sprintf("%v", core.ToLabel)], "ramanGain": core.RamanGain, "totalLoss": 0}
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
		index := fmt.Sprintf("%v", res["ots"].(otsCon).GuiLabel)
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
			log.Println("SUCCESS: Loss report file has been exported!")
		} else {
			panic(err)
		}
	}
}
