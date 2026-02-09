package settings

import "encoding/json"

type shopEmailConfigView struct {
	APIKey string `json:"api_key"`
	Domain string `json:"domain"`
}

func decodeShopEmailConfig(config map[string]any) shopEmailConfigView {
	var out shopEmailConfigView
	if len(config) == 0 {
		return out
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out
	}
	return out
}

func maskAPIKey(value string) string {
	if len(value) <= 4 {
		return value
	}
	return "••••" + value[len(value)-4:]
}
