package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)


type Client struct {
	baseURL string
	httpClient *http.Client
}


func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{},
	}
}


func (c *Client) Query(query string) (map[string]interface{}, error) {

	endpoint := fmt.Sprintf(
		"%s/api/v1/query",
		c.baseURL,
	)

	params := url.Values{}
	params.Set("query", query)

	resp, err := c.httpClient.Get(
		endpoint + "?" + params.Encode(),
	)

	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()


	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}



func (c *Client) QueryRange(
	query string,
	start string,
	end string,
	step string,
) (map[string]interface{}, error) {

	endpoint := fmt.Sprintf(
		"%s/api/v1/query_range",
		c.baseURL,
	)

	params := url.Values{}

	params.Set("query", query)
	params.Set("start", start)
	params.Set("end", end)
	params.Set("step", step)

	resp, err := c.httpClient.Get(
		endpoint+"?"+params.Encode(),
	)

	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}