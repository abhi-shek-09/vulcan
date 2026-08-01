package dashboard

import (
	"strconv"
)

func ParseValue(
	response map[string]interface{},
) float64 {
	data, ok := response["data"].(map[string]interface{})
	if !ok {
		return 0
	}

	result, ok := data["result"].([]interface{})
	if !ok || len(result) == 0 {
		return 0
	}

	first, ok := result[0].(map[string]interface{})
	if !ok {
		return 0
	}

	value, ok := first["value"].([]interface{})
	if !ok || len(value) < 2 {
		return 0
	}
	switch v := value[1].(type) {
	case float64:
		return v
	case string:
		parsed, err := strconv.ParseFloat(v, 64)

		if err != nil {
			return 0
		}

		return parsed
	}
	return 0
}

func ParseSeries(
	response map[string]interface{},
) []DataPoint {

	data, ok := response["data"].(map[string]interface{})
	if !ok {
		return nil
	}

	result, ok := data["result"].([]interface{})
	if !ok || len(result)==0 {
		return nil
	}

	first, ok := result[0].(map[string]interface{})
	if !ok {
		return nil
	}
	values, ok := first["values"].([]interface{})
	if !ok {
		return nil
	}
	points := make([]DataPoint,0)
	for _, item := range values {

		pair, ok := item.([]interface{})

		if !ok || len(pair)<2 {
			continue
		}

		ts, ok := pair[0].(float64)

		if !ok {
			continue
		}

		val, ok := pair[1].(string)

		if !ok {
			continue
		}


		value, err := strconv.ParseFloat(
			val,
			64,
		)

		if err != nil {
			continue
		}
		points = append(
			points,
			DataPoint{
				Timestamp:int64(ts),
				Value:value,
			},
		)
	}
	return points
}