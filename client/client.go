//Package client contains methods to make request to Binance API server.
package client

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

//API is a Binance API client.
type API struct {
	URL           string
	Key           string
	SecretKey     string
	HTTPClient    *http.Client
	UserAgent     string
	AutoReconnect bool
}

const (
	ReconnectLimit = 10
)

//New initializes API with given URL, api key and secret key. it also provides a way to overwrite *http.Client
func New(url, key, secretKey string, httpClient *http.Client, userAgent string) *API {
	return &API{
		URL:           url,
		Key:           key,
		SecretKey:     secretKey,
		HTTPClient:    httpClient,
		UserAgent:     userAgent,
		AutoReconnect: true,
	}
}

//Making a public request to Binance API server.
func (a *API) Request(method, endpoint string, params interface{}, out interface{}) error {
	url, _ := url.ParseRequestURI(a.URL)
	url.Path = url.Path + endpoint

	if method == "GET" {
		//parse params to query string
		b, _ := json.Marshal(params)
		m := map[string]interface{}{}
		json.Unmarshal(b, &m)
		q := url.Query()
		for k, v := range m {
			q.Set(k, fmt.Sprintf("%v", v))
		}
		url.RawQuery = q.Encode()
	}
	log.Printf("%v %v", method, url.String())
	req, _ := http.NewRequest(method, url.String(), nil)

	req.Header.Add("content-type", "application/json")
	req.Header.Add("X-MBX-APIKEY", a.Key)
	req.Header.Add("UserAgent", a.UserAgent)
	res, err := a.HTTPClient.Do(req)
	defer res.Body.Close()
	if res.StatusCode != 200 {
		type binanceError struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		e := binanceError{}
		err = json.NewDecoder(res.Body).Decode(&e)
		return errors.New(e.Msg)
	}

	if out != nil {
		err = json.NewDecoder(res.Body).Decode(&out)
	}

	return err
}

//Making a signed request to Binance API server.
func (a *API) SignedRequest(method, endpoint string, params interface{}, out interface{}) error {
	url, _ := url.ParseRequestURI(a.URL)
	url.Path = url.Path + endpoint

	//parse params to query string
	b, _ := json.Marshal(params)
	m := map[string]interface{}{}
	json.Unmarshal(b, &m)

	q := url.Query()
	for k, v := range m {
		q.Set(k, fmt.Sprintf("%v", v))
	}

	//timestamp is mandatory in signed request
	q.Add("timestamp", fmt.Sprintf("%v", time.Now().Unix()*1000))

	mac := hmac.New(sha256.New, []byte(a.SecretKey))
	mac.Write([]byte(q.Encode()))
	expectedMAC := mac.Sum(nil)
	signed := hex.EncodeToString(expectedMAC)
	//signature needs to be at the last param
	url.RawQuery = q.Encode() + "&signature=" + signed

	log.Printf("%v %v", method, url.String())

	req, _ := http.NewRequest(method, url.String(), nil)

	req.Header.Add("content-type", "application/json")
	req.Header.Add("X-MBX-APIKEY", a.Key)
	req.Header.Add("UserAgent", a.UserAgent)
	res, err := a.HTTPClient.Do(req)

	defer res.Body.Close()
	if res.StatusCode != 200 {
		type binanceError struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		e := binanceError{}
		err = json.NewDecoder(res.Body).Decode(&e)
		return errors.New(e.Msg)
	}
	defer res.Body.Close()
	if out != nil {
		err = json.NewDecoder(res.Body).Decode(&out)
	}
	return err
}

type StreamHandler func(data []byte)

func (a *API) connect(endpoint string) *websocket.Conn {
	url := fmt.Sprintf("wss://stream.binance.com:9443/ws/%s", endpoint)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatal("dial:", err)
		return nil
	}

	return conn
}

func (a *API) Stream(endpoint string, handler StreamHandler) {
	websocketClient := a.connect(endpoint)

	go func() {
		defer websocketClient.Close()
		reconnects := 0
		for {
			_, m, err := websocketClient.ReadMessage()
			if err != nil {
				log.Println("read:", err)
				if a.AutoReconnect && reconnects < ReconnectLimit {
					err := websocketClient.Close()
					if err != nil {
						log.Println("close:", err)
					}

					reconnects++
					websocketClient = a.connect(endpoint)
					continue
				}

				return
			}
			go handler(m)
		}
	}()

}
