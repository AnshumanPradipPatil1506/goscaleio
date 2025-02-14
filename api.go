// Copyright © 2019 - 2022 Dell Inc. or its subsidiaries. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//      http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package goscaleio

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/AnshumanPradipPatil1506/goscaleio/api"
	types "github.com/AnshumanPradipPatil1506/goscaleio/types/v1"
)

var (
	mu        sync.Mutex // guards accHeader and conHeader
	accHeader string
	conHeader string

	errNilReponse = errors.New("nil response from API")
	errBodyRead   = errors.New("error reading body")
	errNoLink     = errors.New("Error: problem finding link")

	debug, _    = strconv.ParseBool(os.Getenv("GOSCALEIO_DEBUG"))
	showHTTP, _ = strconv.ParseBool(os.Getenv("GOSCALEIO_SHOWHTTP"))
)

// Client defines struct for Client
type Client struct {
	configConnect *ConfigConnect
	api           api.Client
	// FringeObject  interface{}
}

// Cluster defines struct for Cluster
type Cluster struct {
}

// ConfigConnect defines struct for ConfigConnect
type ConfigConnect struct {
	Endpoint string
	Version  string
	Username string
	Password string
}

// ClientPersistent defines struct for ClientPersistent
type ClientPersistent struct {
	configConnect *ConfigConnect
	client        *Client
}

// getVersion returns version
func (c *Client) getVersion() (string, error) {

	resp, err := c.api.DoAndGetResponseBody(
		context.Background(), http.MethodGet, "/api/version", nil, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// parse the response
	switch {
	case resp == nil:
		return "", errNilReponse
	case !(resp.StatusCode >= 200 && resp.StatusCode <= 299):
		return "", c.api.ParseJSONError(resp)
	}

	version, err := extractString(resp)
	if err != nil {
		return "", err
	}

	versionRX := regexp.MustCompile(`^(\d+?\.\d+?).*$`)
	if m := versionRX.FindStringSubmatch(version); len(m) > 0 {
		return m[1], nil
	}
	return version, nil
}

// updateVersion updates version
func (c *Client) updateVersion() error {

	version, err := c.getVersion()
	if err != nil {
		return err
	}
	c.configConnect.Version = version

	updateHeaders(version)

	return nil
}

func updateHeaders(version string) {
	mu.Lock()
	defer mu.Unlock()
	accHeader = api.HeaderValContentTypeJSON
	if version != "" {
		accHeader = accHeader + ";version=" + version
	}
	conHeader = accHeader
}

// Authenticate controls authentication to client
func (c *Client) Authenticate(configConnect *ConfigConnect) (Cluster, error) {

	configConnect.Version = c.configConnect.Version
	c.configConnect = configConnect

	c.api.SetToken("")

	headers := make(map[string]string, 1)
	headers["Authorization"] = "Basic " + basicAuth(
		configConnect.Username, configConnect.Password)

	resp, err := c.api.DoAndGetResponseBody(
		context.Background(), http.MethodGet, "api/login", headers, nil)
	if err != nil {
		doLog(log.WithError(err).Error, "")
		return Cluster{}, err
	}
	defer resp.Body.Close()

	// parse the response
	switch {
	case resp == nil:
		return Cluster{}, errNilReponse
	case !(resp.StatusCode >= 200 && resp.StatusCode <= 299):
		return Cluster{}, c.api.ParseJSONError(resp)
	}

	token, err := extractString(resp)
	if err != nil {
		return Cluster{}, nil
	}

	c.api.SetToken(token)

	if c.configConnect.Version == "" {
		err = c.updateVersion()
		if err != nil {
			return Cluster{}, errors.New("error getting version of ScaleIO")
		}
	}

	return Cluster{}, nil
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

func (c *Client) getJSONWithRetry(
	method, uri string,
	body, resp interface{}) error {

	headers := make(map[string]string, 2)
	headers[api.HeaderKeyAccept] = accHeader
	headers[api.HeaderKeyContentType] = conHeader
	addMetaData(headers, body)

	err := c.api.DoWithHeaders(
		context.Background(), method, uri, headers, body, resp)
	if err == nil {
		return nil
	}

	// check if we need to authenticate
	if e, ok := err.(*types.Error); ok {
		doLog(log.WithError(err).Debug, fmt.Sprintf("Got JSON error: %+v", e))
		if e.HTTPStatusCode == 401 {
			doLog(log.Info, "Need to re-auth")
			// Authenticate then try again
			if _, err := c.Authenticate(c.configConnect); err != nil {
				return fmt.Errorf("Error Authenticating: %s", err)
			}
			return c.api.DoWithHeaders(
				context.Background(), method, uri, headers, body, resp)
		}
	}
	doLog(log.WithError(err).Error, "returning error")

	return err
}

func (c *Client) authorizedJSONWithRetry(method string, uri string,
	body interface{}) (interface{}, error) {
	timeout := time.Second * 60
	headers := make(map[string]string)
	headers[api.HeaderKeyAccept] = accHeader
	headers[api.HeaderKeyContentType] = conHeader
	// ctx context.Context,
	// 	method, path string,
	// 	headers map[string]string,
	// 	body, resp interface{}

	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := c.api.DoAndGetResponseBodyAuthorized(ctx, method, uri, headers, body)
	if err == nil {
		return resp, nil
	}
	return resp, err
}

func extractString(resp *http.Response) (string, error) {
	bs, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", errBodyRead
	}

	s := string(bs)

	// Remove any whitespace that might surround the JSON
	// JSON tokens are separated by whitespace, but there is no specification
	// determining what whitespace is to be expected so we have to prepare for the worst
	// see: https://github.com/golang/go/issues/7767#issuecomment-66093559
	s = strings.TrimSpace(s)

	s = strings.TrimLeft(s, `"`)
	s = strings.TrimRight(s, `"`)

	return s, nil
}

func (c *Client) getStringWithRetry(
	method, uri string,
	body interface{}) (string, error) {

	headers := make(map[string]string, 2)
	headers[api.HeaderKeyAccept] = accHeader
	headers[api.HeaderKeyContentType] = conHeader
	addMetaData(headers, body)

	checkResponse := func(resp *http.Response) (string, bool, error) {
		defer resp.Body.Close()

		// parse the response
		switch {
		case resp == nil:
			return "", false, errNilReponse
		case resp.StatusCode == 401:
			return "", true, c.api.ParseJSONError(resp)
		case !(resp.StatusCode >= 200 && resp.StatusCode <= 299):
			return "", false, c.api.ParseJSONError(resp)
		}

		s, err := extractString(resp)
		if err != nil {
			return "", false, err
		}

		return s, false, nil
	}

	resp, err := c.api.DoAndGetResponseBody(
		context.Background(), method, uri, headers, body)
	if err != nil {
		return "", err
	}
	s, retry, httpErr := checkResponse(resp)
	if httpErr != nil {
		if retry {
			doLog(log.Info, "need to re-auth")
			// Authenticate then try again
			if _, err = c.Authenticate(c.configConnect); err != nil {
				return "", fmt.Errorf("Error Authenticating: %s", err)
			}
			resp, err = c.api.DoAndGetResponseBody(
				context.Background(), method, uri, headers, body)
			if err != nil {
				return "", err
			}
			s, _, err = checkResponse(resp)
		} else {
			return "", httpErr
		}
	}

	return s, nil
}

// SetToken sets token
func (c *Client) SetToken(token string) {
	c.api.SetToken(token)
}

// GetToken returns token
func (c *Client) GetToken() string {
	return c.api.GetToken()
}

// NewClient returns a new client
func NewClient() (client *Client, err error) {
	return NewClientWithArgs(
		os.Getenv("GOSCALEIO_ENDPOINT"),
		os.Getenv("GOSCALEIO_VERSION"),
		os.Getenv("GOSCALEIO_INSECURE") == "true",
		os.Getenv("GOSCALEIO_USECERTS") == "true")
}

// NewClientWithArgs returns a new client
func NewClientWithArgs(
	endpoint string,
	version string,
	insecure,
	useCerts bool) (client *Client, err error) {

	if showHTTP {
		debug = true
	}

	fields := map[string]interface{}{
		"endpoint": endpoint,
		"insecure": insecure,
		"useCerts": useCerts,
		"version":  version,
		"debug":    debug,
		"showHTTP": showHTTP,
	}

	doLog(log.WithFields(fields).Debug, "goscaleio client init")

	if endpoint == "" {
		doLog(log.WithFields(fields).Error, "endpoint is required")
		return nil,
			withFields(fields, "endpoint is required")
	}

	opts := api.ClientOptions{
		Insecure: insecure,
		UseCerts: useCerts,
		ShowHTTP: showHTTP,
	}

	ac, err := api.New(context.Background(), endpoint, opts, debug)
	if err != nil {
		doLog(log.WithError(err).Error, "Unable to create HTTP client")
		return nil, err
	}

	client = &Client{
		api: ac,
		configConnect: &ConfigConnect{
			Version: version,
		},
	}

	updateHeaders(version)

	return client, nil
}

// GetLink returns a link
func GetLink(links []*types.Link, rel string) (*types.Link, error) {
	for _, link := range links {
		if link.Rel == rel {
			return link, nil
		}
	}

	return nil, errNoLink
}

// GetLinkFromSdc returns a link created basis on relation
func GetLinkFromSdc(sdc *types.Sdc, rel string) (*types.Link, error) {
	returnLink := types.Link{}
	returnLink.Rel = rel
	switch rel {
	case "/api/Sdc/relationship/Statistics":
		returnLink.HREF = fmt.Sprintf("/api/instances/Sdc::%v/relationships/Statistics", sdc.ID)
	case "/api/Sdc/relationship/Volume":
		returnLink.HREF = fmt.Sprintf("/api/instances/Sdc::%v/relationships/Volume", sdc.ID)
	}

	return &returnLink, nil
}

func withFields(fields map[string]interface{}, message string) error {
	return withFieldsE(fields, message, nil)
}

func withFieldsE(
	fields map[string]interface{}, message string, inner error) error {

	if fields == nil {
		fields = make(map[string]interface{})
	}

	if inner != nil {
		fields["inner"] = inner
	}

	x := 0
	l := len(fields)

	var b bytes.Buffer
	for k, v := range fields {
		if x < l-1 {
			b.WriteString(fmt.Sprintf("%s=%v,", k, v))
		} else {
			b.WriteString(fmt.Sprintf("%s=%v", k, v))
		}
		x = x + 1
	}

	return fmt.Errorf("%s %s", message, b.String())
}

func doLog(
	l func(args ...interface{}),
	msg string) {

	if debug {
		l(msg)
	}
}

// ExternalTimeRecorder is used to track time
var ExternalTimeRecorder func(string, time.Duration)

// TimeSpent is used to track time spent
func TimeSpent(functionName string, startTime time.Time) {
	if ExternalTimeRecorder != nil {
		endTime := time.Now()
		ExternalTimeRecorder(functionName, endTime.Sub(startTime))
	}
}

func addMetaData(headers map[string]string, body interface{}) {
	if headers == nil || body == nil {
		return
	}
	// If the body contains a MetaData method, extract the data
	// and add as HTTP headers.
	if vp, ok := interface{}(body).(interface {
		MetaData() http.Header
	}); ok {
		for k := range vp.MetaData() {
			headers[k] = vp.MetaData().Get(k)
		}
	}
}
