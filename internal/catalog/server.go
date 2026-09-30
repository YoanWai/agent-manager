package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

// serverAddress returns announced's first group from the server's ready line.
func serverAddress(ctx context.Context, proc *process, announced *regexp.Regexp) (string, error) {
	for {
		line, err := proc.next(ctx)
		if err != nil {
			return "", err
		}
		if match := announced.FindSubmatch(line); match != nil {
			return string(match[1]), nil
		}
	}
}

func getJSON(ctx context.Context, url string, auth func(*http.Request), out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	auth(request)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("GET %s: %s %s", request.URL.Path, response.Status, body)
	}
	return json.NewDecoder(response.Body).Decode(out)
}
