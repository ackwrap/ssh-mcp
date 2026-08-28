package mcpserver

import "encoding/json"

func jsonMarshal(value any) ([]byte, error)      { return json.Marshal(value) }
func jsonUnmarshal(data []byte, value any) error { return json.Unmarshal(data, value) }
