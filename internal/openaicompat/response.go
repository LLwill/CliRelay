package openaicompat

import "github.com/tidwall/gjson"

// ResponseRoot unwraps provider envelopes such as ClinePass
// {"success":true,"data":{...openai response...}}.
func ResponseRoot(root gjson.Result) gjson.Result {
	data := root.Get("data")
	if !data.Exists() || !data.IsObject() {
		return root
	}
	if data.Get("choices").Exists() || data.Get("usage").Exists() || data.Get("output").Exists() || data.Get("model").Exists() || data.Get("id").Exists() {
		return data
	}
	return root
}

func ParseResponseRoot(data []byte) gjson.Result {
	return ResponseRoot(gjson.ParseBytes(data))
}
