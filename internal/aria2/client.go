package aria2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

type Client struct {
	rpcURL string
	secret string
	id     atomic.Int64
	client *http.Client
}

type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int64         `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Status struct {
	GID            string `json:"gid"`
	Status         string `json:"status"`
	TotalLength    string `json:"totalLength"`
	CompletedLength string `json:"completedLength"`
	DownloadSpeed  string `json:"downloadSpeed"`
	ErrorCode      string `json:"errorCode"`
	ErrorMessage   string `json:"errorMessage"`
	Files          []struct {
		Index  string `json:"index"`
		Path   string `json:"path"`
		Length string `json:"length"`
		URIs   []struct {
			Status string `json:"status"`
			URI    string `json:"uri"`
		} `json:"uris"`
	} `json:"files"`
}

type GlobalStat struct {
	DownloadSpeed   string `json:"downloadSpeed"`
	UploadSpeed     string `json:"uploadSpeed"`
	NumActive       string `json:"numActive"`
	NumWaiting      string `json:"numWaiting"`
	NumStopped      string `json:"numStopped"`
	NumStoppedTotal string `json:"numStoppedTotal"`
}

func NewClient(rpcURL, secret string) *Client {
	if rpcURL == "" {
		rpcURL = "http://127.0.0.1:6800/jsonrpc"
	}
	return &Client{
		rpcURL: rpcURL,
		secret: secret,
		client: &http.Client{},
	}
}

func (c *Client) call(method string, params ...interface{}) (json.RawMessage, error) {
	id := c.id.Add(1)

	allParams := make([]interface{}, 0, len(params)+1)
	if c.secret != "" {
		allParams = append(allParams, "token:"+c.secret)
	}
	allParams = append(allParams, params...)

	reqBody := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  allParams,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.client.Post(c.rpcURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("send request to aria2: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aria2 rpc returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("aria2 error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

func (c *Client) AddURI(uris []string, options map[string]interface{}) (string, error) {
	if options == nil {
		options = map[string]interface{}{}
	}
	result, err := c.call("aria2.addUri", uris, options)
	if err != nil {
		return "", err
	}

	var gid string
	if err := json.Unmarshal(result, &gid); err != nil {
		return "", fmt.Errorf("unmarshal gid: %w", err)
	}
	return gid, nil
}

func (c *Client) TellStatus(gid string) (*Status, error) {
	result, err := c.call("aria2.tellStatus", gid)
	if err != nil {
		return nil, err
	}

	var status Status
	if err := json.Unmarshal(result, &status); err != nil {
		return nil, fmt.Errorf("unmarshal status: %w", err)
	}
	return &status, nil
}

func (c *Client) TellActive() ([]Status, error) {
	result, err := c.call("aria2.tellActive")
	if err != nil {
		return nil, err
	}

	var statuses []Status
	if err := json.Unmarshal(result, &statuses); err != nil {
		return nil, fmt.Errorf("unmarshal active: %w", err)
	}
	return statuses, nil
}

func (c *Client) TellWaiting(offset, num int) ([]Status, error) {
	result, err := c.call("aria2.tellWaiting", offset, num)
	if err != nil {
		return nil, err
	}

	var statuses []Status
	if err := json.Unmarshal(result, &statuses); err != nil {
		return nil, fmt.Errorf("unmarshal waiting: %w", err)
	}
	return statuses, nil
}

func (c *Client) TellStopped(offset, num int) ([]Status, error) {
	result, err := c.call("aria2.tellStopped", offset, num)
	if err != nil {
		return nil, err
	}

	var statuses []Status
	if err := json.Unmarshal(result, &statuses); err != nil {
		return nil, fmt.Errorf("unmarshal stopped: %w", err)
	}
	return statuses, nil
}

func (c *Client) Remove(gid string) (string, error) {
	result, err := c.call("aria2.remove", gid)
	if err != nil {
		return "", err
	}

	var removedGID string
	if err := json.Unmarshal(result, &removedGID); err != nil {
		return "", fmt.Errorf("unmarshal removed gid: %w", err)
	}
	return removedGID, nil
}

func (c *Client) Pause(gid string) (string, error) {
	result, err := c.call("aria2.pause", gid)
	if err != nil {
		return "", err
	}

	var pausedGID string
	if err := json.Unmarshal(result, &pausedGID); err != nil {
		return "", fmt.Errorf("unmarshal paused gid: %w", err)
	}
	return pausedGID, nil
}

func (c *Client) Unpause(gid string) (string, error) {
	result, err := c.call("aria2.unpause", gid)
	if err != nil {
		return "", err
	}

	var unpausedGID string
	if err := json.Unmarshal(result, &unpausedGID); err != nil {
		return "", fmt.Errorf("unmarshal unpaused gid: %w", err)
	}
	return unpausedGID, nil
}

func (c *Client) GetGlobalStat() (*GlobalStat, error) {
	result, err := c.call("aria2.getGlobalStat")
	if err != nil {
		return nil, err
	}

	var stat GlobalStat
	if err := json.Unmarshal(result, &stat); err != nil {
		return nil, fmt.Errorf("unmarshal global stat: %w", err)
	}
	return &stat, nil
}

func (c *Client) GetVersion() (map[string]interface{}, error) {
	result, err := c.call("aria2.getVersion")
	if err != nil {
		return nil, err
	}

	var version map[string]interface{}
	if err := json.Unmarshal(result, &version); err != nil {
		return nil, fmt.Errorf("unmarshal version: %w", err)
	}
	return version, nil
}

func (c *Client) Shutdown() error {
	_, err := c.call("aria2.shutdown")
	return err
}
