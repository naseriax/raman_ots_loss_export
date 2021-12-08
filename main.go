package main

/* This script is used to calculate optical fiber loss values from a more
prefered point in case desired.The loss values can be retrieved from Nokia NFM-T's UI :
	Operate > Physical Connections > OTS 360 View > Fiber Characteristics.

Firstly, This manual calculation through NFM-T's Rest API is useful in case we want to get
the current loss values for all/many OTS connections in one shot which is not possible via NFM-T's UI.

Secondly, Some operators may prefer to use the Raman card's Linein power instead
of the LD's Linein to calculate the loss and that's how this script calculates the
loss for the fiber's configured with Raman Amplifiers (RA2P).
*/

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

//fiberCore includes the required fields from NFM-T's Fiber Characteristics' json response.
type fiberCore struct {
	FromLabel      string `json:"fromLabel"`
	ToLabel        string `json:"toLabel"`
	EgressPowerOut string `json:"egressPowerOut"`
	IngressPowerIn string `json:"ingressPowerIn"`
	RamanGain      string `json:"targetGainStr"`
}

type nxPmData struct {
	CxnName string `json:"cxnName"`
	CxnId   string `json:"cxnId"`
}

//otsCon includes the required fields from NFM-T's physical connections' json response.
type otsCon struct {
	APortLabel        string `json:"aPortLabel"`
	ZPortLabel        string `json:"zPortLabel"`
	A2PortLabel       string `json:"a2PortLabel"`
	Z2PortLabel       string `json:"z2PortLabel"`
	WdmConnectionType string `json:"wdmConnectionType"`
	GuiLabel          string `json:"guiLabel"`
	Id                int    `json:"id"`
}

//pmStruct includes the required fields from NFM-T's NextGen PM query for the OTS connection.
type pmStruct struct {
	ObjGraphDataMap []struct {
		GraphDataMap struct {
			OPINTOPRAVGReceiveNEND struct {
				Pmdata []map[string]interface{} `json:"pmdata"`
			} `json:"OPIN/TOPR-AVG (Receive/NEND)"`
		} `json:"graphDataMap"`
	} `json:"objGraphDataMap"`
}

//GetRamanConnections fills the []otsCon struct with the OTS connections having the specified ldType.
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

//getFiberChar fills the []fiberCore struct with the Fiber Characteristics information for the provided OTS.
func getFiberChar(agent RestAgent, ots otsCon) []fiberCore {
	log.Printf("Retrieving the Fiber Characteristics for OTS: %v\n", fmt.Sprintf("%v", ots.GuiLabel))
	resp := agent.HttpGet(fmt.Sprintf("/data/npr/physicalConns/%v/fiberCharacteristic", fmt.Sprintf("%v", ots.Id)), map[string]string{})
	var fiberCharInfo []fiberCore
	json.Unmarshal([]byte(resp), &fiberCharInfo)
	return fiberCharInfo
}

//nxPmConList returns the connections' names managed under Next-Gen PM application in NFM-T.
func nxPmConList(agent RestAgent) []nxPmData {
	url := "/mncpm/mdcxnlist/"
	payload := map[string]int{
		"noOfEntries": 10000,
		"startIndex":  0,
	}
	nxPmRawData := agent.HttpPostJson(url, payload, map[string]string{})
	var nxPmJsonData []nxPmData
	json.Unmarshal([]byte(nxPmRawData), &nxPmJsonData)
	return nxPmJsonData
}

//timeCalculator Provides current time in Epoch format to be used for PM Query payload construction.
func timeCalculator() [2]int {
	ts := time.Now().Unix()
	return [2]int{int(ts), int(ts) - 3600}
}

//getPortPower returns the Performance Data for the specified []portInfo.
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
			var dataContainer, val interface{}
			if v, ok := data[port]; ok {
				dataContainer = data[port]
				val = v
			} else if v, ok := data[port+"(Z End)"]; ok {
				dataContainer = data[port+"(Z End)"]
				val = v
			}
			if val != "" {
				if len(t) == 0 {
					t[port] = fmt.Sprintf("%v", dataContainer)
					t["Time"] = fmt.Sprintf("%v", data["Time"])
				} else {
					layout := "01/02/2006 15:04"
					t1, _ := time.Parse(layout, t["Time"])
					t2, _ := time.Parse(layout, fmt.Sprintf("%v", data["Time"]))
					if t1.Unix() < t2.Unix() {
						t[port] = fmt.Sprintf("%v", dataContainer)
						t["Time"] = fmt.Sprintf("%v", data["Time"])
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

//exportFile exportes the calculated values to CSV.
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

//coreLossCalculator calculates the loss info for the specified core.
func coreLossCalculator(restAgent RestAgent, ots otsCon, ldType string, core fiberCore) map[string]interface{} {
	//nxPm has all the connections listed in Next-Gen PM application in NFM-T.
	nxPm := nxPmConList(restAgent)
	var otsPmId nxPmData
	for _, item := range nxPm {
		if item.CxnName == ots.GuiLabel {
			otsPmId = item
		}
	}
	pmData := getPortPower(
		restAgent,
		[]string{
			fmt.Sprintf("%v", ots.Z2PortLabel),
			fmt.Sprintf("%v", ots.ZPortLabel)},
		fmt.Sprintf("%v", otsPmId.CxnId),
	)
	if pmData == nil {
		return nil
	}
	corePowerData := map[string]interface{}{
		"egressPort":  core.FromLabel,
		"ingressPort": core.ToLabel,
		"egressPower": core.EgressPowerOut,
		"ramanGain":   core.RamanGain,
		"totalLoss":   0,
	}
	if strings.Contains("RA2P", ldType) {
		corePowerData["ingressPower"] = pmData[fmt.Sprintf("%v", core.ToLabel)]
	} else {
		corePowerData["ingressPower"] = core.IngressPowerIn
	}

	egPower, err := strconv.ParseFloat(fmt.Sprintf("%v", corePowerData["egressPower"]), 64)
	errDealer(err)
	inPower, err := strconv.ParseFloat(fmt.Sprintf("%v", corePowerData["ingressPower"]), 64)
	errDealer(err)

	if corePowerData["ramanGain"] != "N.A." {
		ramanGain, err := strconv.ParseFloat(fmt.Sprintf("%v", corePowerData["ramanGain"]), 64)
		errDealer(err)
		corePowerData["totalLoss"] = (egPower - inPower + ramanGain) * 1000 / 1000
	} else {
		corePowerData["totalLoss"] = (egPower - inPower) * 1000 / 1000
	}

	return corePowerData
}

func main() {
	var ipaddr, uname, passw, ldType string
	flag.StringVar(&uname, "u", "admin", "Specify the username. Default is admin.")
	flag.StringVar(&passw, "p", "", "Specify the password.")
	flag.StringVar(&ipaddr, "i", "127.0.0.1", "Specify NFM-T IP Address. Default is 127.0.0.1.")
	flag.StringVar(&ldType, "l", "RA2P", "Specify The LD type. Default is RA2P.")
	flag.Usage = func() {
		fmt.Printf("Usage: \n")
		fmt.Printf("./otsloss.exe -u admin -p password -i 192.168.0.1 -l RA2P \n")
	}
	flag.Parse()

	var mainWG sync.WaitGroup
	var subWG sync.WaitGroup

	//masterfile will collect all the calculated values and will be used to generate the output file.
	masterfile := []map[string]interface{}{}

	//restAgent will open a Rest Session towards NFM-T and will be used for any Get/Post queries.
	restAgent := Init(ipaddr, uname, passw)
	defer restAgent.NfmtDeauth()

	//otsList Contains the otsCon struct for every OTS connection which has the specified ldType.
	otsList := GetRamanConnections(restAgent, ldType)

	//For each OTS connection in otsList, we query the fiber characteristics and PM info(1 operation for each core of the OTS).
	for _, ots := range otsList {
		mainWG.Add(1)
		go func(ots otsCon) {
			chars := getFiberChar(restAgent, ots)
			var cores []map[string]interface{}
			for _, core := range chars {
				subWG.Add(1)
				go func(core fiberCore) {
					corePowerData := coreLossCalculator(restAgent, ots, ldType, core)
					if corePowerData == nil {
						subWG.Done()
						return
					}
					cores = append(cores, corePowerData)
					subWG.Done()
				}(core)
			}
			subWG.Wait()
			masterfile = append(masterfile, map[string]interface{}{"ots": ots, "cores": cores})
			mainWG.Done()
		}(ots)
	}
	mainWG.Wait()
	output := [][]string{
		{
			"OTS Name",
			"Egress Port",
			"Egress Power",
			"Ingress Port",
			"Ingress Power",
			"Raman Gain",
			"Total Loss",
		},
	}
	for _, res := range masterfile {
		index := fmt.Sprintf("%v", res["ots"].(otsCon).GuiLabel)
		for _, co := range res["cores"].([]map[string]interface{}) {
			rowToAdd := []string{
				index,
				fmt.Sprintf("%v", co["egressPort"]),
				fmt.Sprintf("%v", co["egressPower"]),
				fmt.Sprintf("%v", co["ingressPort"]),
				fmt.Sprintf("%v", co["ingressPower"]),
				fmt.Sprintf("%v", co["ramanGain"]),
				fmt.Sprintf("%v", co["totalLoss"]),
			}
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
