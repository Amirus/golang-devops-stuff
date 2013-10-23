package replay

import (
	"net/http"
	"net/url"
	"time"
)

// HttpTiming contains timestamps for http requests and responses
type HttpTiming struct {
	reqStart time.Time
	respDone time.Time
}

// HttpResponse contains a host, a http request,
// a http response and an error
type HttpResponse struct {
	host   *ForwardHost
	req    *http.Request
	resp   *http.Response
	err    error
	timing *HttpTiming
}

// RequestFactory processes requests
//
// Basic workflow:
//
// 1. When request added via Add() it get pushed to `responses` chan
// 2. handleRequest() listen for `responses` chan and decide where request should be forwarded, and apply rate-limit if needed
// 3. sendRequest() forwards request and returns response info to `responses` chan
// 4. handleRequest() listen for `response` channel and updates stats
type RequestFactory struct {
	c_responses chan *HttpResponse
	c_requests  chan []byte
}

// NewRequestFactory returns a RequestFactory pointer
// One created, it starts listening for incoming requests: requests channel
func NewRequestFactory() (factory *RequestFactory) {
	factory = &RequestFactory{}
	factory.c_responses = make(chan *HttpResponse)
	factory.c_requests = make(chan []byte)

	go factory.handleRequests()

	return
}

type RedirectNotAllowed struct{}

func (e *RedirectNotAllowed) Error() string {
	return "Redirects not allowed"
}

// customCheckRedirect disables redirects https://github.com/buger/gor/pull/15
func customCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 0 {
		return new(RedirectNotAllowed)
	}
	return nil
}

// sendRequest forwards http request to a given host
func (f *RequestFactory) sendRequest(host *ForwardHost, requestBytes []byte) {
	client := &http.Client{
		CheckRedirect: customCheckRedirect,
	}

	request, _ := ParseRequest(requestBytes)

	// Change HOST of original request
	URL := host.Url + request.URL.Path + "?" + request.URL.RawQuery

	request.RequestURI = ""
	request.URL, _ = url.ParseRequestURI(URL)

	for _, header := range Settings.AdditionalHeaders {
		request.Header.Set(header.Name, header.Value)
	}

	Debug("Sending request:", host.Url, request)

	tstart := time.Now()
	resp, err := client.Do(request)
	tstop := time.Now()

	// We should not count Redirect as errors
	if _, ok := err.(*RedirectNotAllowed); ok {
		err = nil
	}

	if err == nil {
		defer resp.Body.Close()
	} else {
		Debug("Request error:", err)
	}

	f.c_responses <- &HttpResponse{host, request, resp, err, &HttpTiming{tstart, tstop}}
}

// handleRequests and their responses
func (f *RequestFactory) handleRequests() {
	hosts := Settings.ForwardedHosts()

	for {
		select {
		case req := <-f.c_requests:
			for _, host := range hosts {
				// Ensure that we have actual stats for given timestamp
				host.Stat.Touch()

				if host.Limit == 0 || host.Stat.Count < host.Limit {
					// Increment Stat.Count
					host.Stat.IncReq()

					go f.sendRequest(host, req)
				}
			}
		case resp := <-f.c_responses:
			// Increment returned http code stats, and elapsed time
			resp.host.Stat.IncResp(resp)

			// Send data to StatsD, ElasticSearch, etc...
			for _, rap := range Settings.ResponseAnalyzePlugins {
				go rap.ResponseAnalyze(resp)
			}
		}
	}

}

// Add request to channel for further processing
func (f *RequestFactory) Add(request []byte) {
	f.c_requests <- request
}
