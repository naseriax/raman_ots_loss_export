package main

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
)

//Token is the object which contains the nfmt single step authentication tokens and the auth and deauth methodes.
type RestAgent struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	Expiry       float64 `json:"expires_in"`
	TokenType    string  `json:"token_type"`
	IpAddress    string
	UserName     string
	Password     string
	Httpclient   *http.Client
}

//NfmtAuth methode does the NFM-T Single step authentication and fills the token variables.
func (t *RestAgent) NfmtAuth() {

	payload := strings.NewReader("grant_type=client_credentials")
	req, err := http.NewRequest("POST", fmt.Sprintf("https://%v/rest-gateway/rest/api/v1/auth/token", t.IpAddress), payload)
	errDealer(err)

	req.Header.Add("Authorization", "Basic "+t.toBase64())
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.Httpclient.Do(req)
	errDealer(err)

	if resp.StatusCode != 200 {
		log.Fatalf("Rest API Authentication Failure: %v %v", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	errDealer(err)

	json.Unmarshal([]byte(body), &t)

	log.Println("REST API Authentication: SUCCESS!")
}

//NfmtDeauth does the deauthentication from the NFM-T.
func (t *RestAgent) NfmtDeauth() {

	payload := strings.NewReader(fmt.Sprintf("token=%v&token_type_hint=token", t.AccessToken))
	req, err := http.NewRequest("POST", fmt.Sprintf("https://%v/rest-gateway/rest/api/v1/auth/revocation", t.IpAddress), payload)
	errDealer(err)

	req.Header.Add("Authorization", "Basic "+t.toBase64())
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.Httpclient.Do(req)
	errDealer(err)
	if resp.StatusCode != 200 {
		log.Fatalf("Rest API De-Authentication Failure: %v %v", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	log.Println("REST API DeAuthentication: SUCCESS!")
	defer resp.Body.Close()
}

//HttpGet send a Get request and returns the response in json string.
func (t *RestAgent) HttpGet(url string, header map[string]string) string {

	req, err := http.NewRequest("GET", fmt.Sprintf("https://%v:8443/oms1350%v", t.IpAddress, url), nil)
	errDealer(err)

	for k, v := range header {
		req.Header.Add(k, v)
	}

	req.Header.Add("Authorization", fmt.Sprintf("%v %v", t.TokenType, t.AccessToken))
	req.Header.Add("Content-Type", "application/json")

	res, err := t.Httpclient.Do(req)
	errDealer(err)

	defer res.Body.Close()
	if res.StatusCode != 200 {
		log.Fatalf("Get Request Failure: %v %v", res.StatusCode, http.StatusText(res.StatusCode))
	}
	body, err := ioutil.ReadAll(res.Body)
	errDealer(err)

	return string(body)
}

func (t *RestAgent) HttpPostJson(url, payload interface{}, header map[string]string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(payload); err != nil {
		log.Fatalf("error - Can't encode the payload - %s", err)
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("https://%v:8443%v", t.IpAddress, url), &buf)
	errDealer(err)

	for k, v := range header {
		req.Header.Add(k, v)
	}
	req.Header.Add("Authorization", fmt.Sprintf("%v %v", t.TokenType, t.AccessToken))
	req.Header.Add("Content-Type", "application/json")

	resp, err := t.Httpclient.Do(req)
	errDealer(err)

	if resp.StatusCode != 200 {
		log.Fatalf("Post Request failure : %v %v", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	errDealer(err)

	return string(body)
}

//toBase64 encodes the user/pass combination to Base64.
func (t *RestAgent) toBase64() string {
	auth := t.UserName + ":" + t.Password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

//errDealer panics with error, as a reusable error checking function.
func errDealer(err error) {
	if err != nil {
		panic(err)
	}
}

//HttpClientCreator creates and returns an unsecure http client object.
func HttpClientCreator() *http.Client {
	customTr := http.DefaultTransport.(*http.Transport).Clone()
	customTr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return &http.Client{Transport: customTr}
}

func Init(ipaddr, uname, passw string) RestAgent {
	token := RestAgent{
		Httpclient: HttpClientCreator(),
		IpAddress:  ipaddr,
		UserName:   uname,
		Password:   passw,
	}
	token.NfmtAuth()
	return token
}
