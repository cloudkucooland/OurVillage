package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type WeatherData struct {
	Weather     string  `json:"weather"`
	Temperature float64 `json:"temperature"`
}

type OpenWeatherResponse struct {
	Weather []struct {
		ID          int    `json:"id"`
		Main        string `json:"main"`
		Description string `json:"description"`
	} `json:"weather"`
	Main struct {
		Temp float64 `json:"temp"`
	} `json:"main"`
	Rain struct {
		OneH   float64 `json:"1h"`
		ThreeH float64 `json:"3h"`
	} `json:"rain"`
	Snow struct {
		OneH   float64 `json:"1h"`
		ThreeH float64 `json:"3h"`
	} `json:"snow"`
	Wind struct {
		Speed float64 `json:"speed"`
	} `json:"wind"`
	Clouds struct {
		All int `json:"all"`
	} `json:"clouds"`
}

type WeatherClient interface {
	FetchWeather(ctx context.Context) (WeatherData, error)
}

type OpenWeatherClient struct {
	APIKey string
	Lat    float64
	Lon    float64
	Client *http.Client
}

func NewOpenWeatherClient(apiKey string, lat, lon float64) *OpenWeatherClient {
	return &OpenWeatherClient{
		APIKey: apiKey,
		Lat:    lat,
		Lon:    lon,
		Client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *OpenWeatherClient) FetchWeather(ctx context.Context) (WeatherData, error) {
	if c.APIKey == "" {
		return WeatherData{}, fmt.Errorf("openweathermap api key is not set")
	}

	u := fmt.Sprintf("https://api.openweathermap.org/data/2.5/weather?lat=%f&lon=%f&appid=%s&units=metric", c.Lat, c.Lon, url.QueryEscape(c.APIKey))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return WeatherData{}, err
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return WeatherData{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return WeatherData{}, fmt.Errorf("openweathermap api returned status %d", resp.StatusCode)
	}

	var owr OpenWeatherResponse
	if err := json.NewDecoder(resp.Body).Decode(&owr); err != nil {
		return WeatherData{}, err
	}

	temp := owr.Main.Temp
	weather := MapOpenWeatherToGame(owr)

	return WeatherData{
		Weather:     weather,
		Temperature: temp,
	}, nil
}

func MapOpenWeatherToGame(owr OpenWeatherResponse) string {
	temp := owr.Main.Temp
	rain1h := owr.Rain.OneH
	snow1h := owr.Snow.OneH
	windSpeed := owr.Wind.Speed
	clouds := owr.Clouds.All

	mainCond := ""
	if len(owr.Weather) > 0 {
		mainCond = strings.ToLower(owr.Weather[0].Main)
	}

	if snow1h > 0 || (temp < 0 && (strings.Contains(mainCond, "snow") || rain1h == 0)) {
		if temp < 0 && (strings.Contains(mainCond, "snow") || snow1h > 0) {
			return "snow"
		}
		if temp < 0 {
			return "frost"
		}
		return "snow"
	}
	if rain1h > 2.0 || strings.Contains(mainCond, "thunderstorm") {
		if rain1h > 5.0 || strings.Contains(mainCond, "thunderstorm") {
			return "storm"
		}
		return "rain"
	}
	if rain1h > 0 || strings.Contains(mainCond, "rain") || strings.Contains(mainCond, "drizzle") {
		return "rain"
	}
	if temp > 35.0 || mainCond == "heatwave" {
		return "heatwave"
	}
	if strings.Contains(mainCond, "fog") || strings.Contains(mainCond, "mist") || strings.Contains(mainCond, "haze") {
		return "fog"
	}
	if windSpeed > 12.0 {
		return "windy"
	}
	if clouds > 80 || strings.Contains(mainCond, "cloud") {
		return "cloudy"
	}
	if temp < 0 {
		return "frost"
	}
	return "clear"
}

func (s *Server) startWeatherPolling(ctx context.Context) {
	if s.config == nil || s.config.Weather.APIKey == "" {
		log.Println("[Weather] OpenWeatherMap API key not configured; running with default static/admin weather.")
		return
	}

	client := NewOpenWeatherClient(s.config.Weather.APIKey, s.config.Weather.Lat, s.config.Weather.Lon)
	interval := s.config.Weather.PollInterval
	if interval <= 0 {
		interval = 15 * time.Minute
	}

	log.Printf("[Weather] Starting OpenWeatherMap poller for lat=%.4f, lon=%.4f with interval=%v", s.config.Weather.Lat, s.config.Weather.Lon, interval)

	// Fetch immediately on startup
	s.pollAndApplyWeather(ctx, client)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pollAndApplyWeather(ctx, client)
		}
	}
}

func (s *Server) pollAndApplyWeather(ctx context.Context, client WeatherClient) {
	data, err := client.FetchWeather(ctx)
	if err != nil {
		log.Printf("[Weather] Failed to fetch live weather from OpenWeatherMap: %v", err)
		return
	}

	s.mu.Lock()
	s.weather = data.Weather
	s.temperature = data.Temperature
	s.mu.Unlock()

	log.Printf("[Weather] Updated live weather: %s, Temp: %.1f°C", data.Weather, data.Temperature)
}
