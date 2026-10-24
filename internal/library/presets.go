package library

// Preset prefills a data source: the website's templates and the AI's
// list_templates. Presets are only shortcuts; every source is just a request
// (with an optional token) whose JSON you map onto screens.
type PresetParam struct {
	K      string `json:"k"`
	V      string `json:"v"`
	Secret bool   `json:"secret,omitempty"`
}

type Preset struct {
	Name     string        `json:"name"`
	Desc     string        `json:"desc"`
	Badge    string        `json:"badge"` // 2-letter mark on the template card
	Method   string        `json:"method"`
	URL      string        `json:"url"`
	Params   []PresetParam `json:"params,omitempty"`
	Body     string        `json:"body,omitempty"`
	Headers  []PresetParam `json:"headers,omitempty"`
	AuthHint string        `json:"auth_hint,omitempty"`
	Interval int           `json:"interval,omitempty"`
}

var Presets = []Preset{
	{Name: "Blank request", Desc: "Any URL that returns JSON", Badge: "{}", Method: "GET"},
	{Name: "Meteosource weather", Desc: "Current weather; free key from meteosource.com", Badge: "MS", Method: "GET",
		URL:    "https://www.meteosource.com/api/v1/free/point?lat=11.2588&lon=75.7804&sections=current&timezone=auto&language=en&units=metric",
		Params: []PresetParam{{K: "key", Secret: true}}, Interval: 900},
	{Name: "Open-Meteo weather", Desc: "Temperature + hourly forecast, no key", Badge: "OM", Method: "GET",
		URL: "https://api.open-meteo.com/v1/forecast?latitude=12.97&longitude=77.59&current=temperature_2m,wind_speed_10m&hourly=temperature_2m&forecast_days=1", Interval: 900},
	{Name: "PostHog SQL", Desc: "Run a HogQL query on your project", Badge: "PH", Method: "POST",
		URL:      "https://us.posthog.com/api/projects/PROJECT_ID/query/",
		Body:     `{"query": {"kind": "HogQLQuery", "query": "SELECT count() FROM events WHERE timestamp > now() - INTERVAL 1 DAY"}}`,
		AuthHint: "Personal API key (phx_...)", Interval: 300},
	{Name: "PostHog insight", Desc: "Results of a saved insight", Badge: "PH", Method: "GET",
		URL: "https://us.posthog.com/api/projects/PROJECT_ID/insights/INSIGHT_ID/?refresh=blocking", AuthHint: "Personal API key (phx_...)", Interval: 300},
	{Name: "HubSpot top deals", Desc: "Biggest deals by amount", Badge: "HS", Method: "POST",
		URL:      "https://api.hubapi.com/crm/v3/objects/deals/search",
		Body:     `{"limit": 10, "properties": ["dealname", "amount", "dealstage"], "sorts": [{"propertyName": "amount", "direction": "DESCENDING"}]}`,
		AuthHint: "Private app token (pat-...)", Interval: 300},
	{Name: "HubSpot contact count", Desc: "Total contacts in your CRM", Badge: "HS", Method: "POST",
		URL: "https://api.hubapi.com/crm/v3/objects/contacts/search", Body: `{"limit": 1}`, AuthHint: "Private app token (pat-...)", Interval: 900},
	{Name: "Crypto prices", Desc: "CoinGecko prices + 24h change, no key", Badge: "CG", Method: "GET",
		URL: "https://api.coingecko.com/api/v3/simple/price?ids=bitcoin,ethereum&vs_currencies=usd&include_24hr_change=true", Interval: 300},
	{Name: "GitHub repo", Desc: "Stars, forks, open issues", Badge: "GH", Method: "GET", URL: "https://api.github.com/repos/PostHog/posthog", Interval: 1800},
}
