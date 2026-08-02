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
	// VictoriaMetrics hides samples ingested in the last 30s by default
	// (-search.latencyOffset). Our aggregator writes every second and
	// stops writing the moment a test ends, so the freshest sample is
	// almost always inside that window — without this, /api/v1/query
	// returns an empty result for any test queried shortly after its
	// last write. Disable the offset for this query.
	params.Set("latency_offset", "1ms")

	fmt.Println("================================")
	fmt.Println("QUERY:", query)
	fmt.Println("URL:", endpoint+"?"+params.Encode())

	resp, err := c.httpClient.Get(
		endpoint + "?" + params.Encode(),
	)

	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)

	fmt.Printf("RESPONSE: %+v\n", result)
	fmt.Println("================================")

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
	// Same reasoning as Query: don't hide the most recent points.
	params.Set("latency_offset", "1ms")

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