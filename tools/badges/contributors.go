package main

import (
	"bytes"
	"fmt"
	"html"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
)

const avatarSize = 64

type contributor struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
	HTMLURL   string `json:"html_url"`
	Type      string `json:"type"`
}

// GitHub's contributors endpoint omits commit co-authors.
var additionalContributors = []contributor{
	{
		Login:     "reddeye1337",
		AvatarURL: "https://avatars.githubusercontent.com/u/261536237?v=4",
		HTMLURL:   "https://github.com/reddeye1337",
		Type:      "User",
	},
	{
		Login:     "mikaoelitiana",
		AvatarURL: "https://avatars.githubusercontent.com/u/674667?v=4",
		HTMLURL:   "https://github.com/mikaoelitiana",
		Type:      "User",
	},
}

func refreshContributors() error {
	var contributors []contributor
	if err := get("https://api.github.com/repos/"+repo+"/contributors?per_page=100", &contributors); err != nil {
		fmt.Printf("::warning::contributors were unavailable, so the README list was left alone: %v\n", err)
		return nil
	}
	contributors = addContributors(humanContributors(contributors), additionalContributors)
	avatars := make([][]byte, len(contributors))
	for i, contributor := range contributors {
		avatar, err := circularAvatar(contributor.AvatarURL)
		if err != nil {
			fmt.Printf("::warning::the avatar for %s was unavailable, so the README list was left alone: %v\n", contributor.Login, err)
			return nil
		}
		avatars[i] = avatar
	}
	if err := writeContributorAvatars(contributors, avatars); err != nil {
		return err
	}
	return fillContributors(contributors)
}

func humanContributors(contributors []contributor) []contributor {
	humans := make([]contributor, 0, len(contributors))
	for _, contributor := range contributors {
		if contributor.Type != "Bot" {
			humans = append(humans, contributor)
		}
	}
	return humans
}

func addContributors(contributors, additional []contributor) []contributor {
	seen := make(map[string]bool, len(contributors)+len(additional))
	result := make([]contributor, 0, len(contributors)+len(additional))
	for _, group := range [][]contributor{contributors, additional} {
		for _, contributor := range group {
			if !seen[contributor.Login] {
				seen[contributor.Login] = true
				result = append(result, contributor)
			}
		}
	}
	return result
}

func circularAvatar(avatarURL string) ([]byte, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s&s=%d", avatarURL, avatarSize))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", avatarURL, resp.Status)
	}
	source, _, err := image.Decode(resp.Body)
	if err != nil {
		return nil, err
	}

	var output bytes.Buffer
	if err := png.Encode(&output, roundAvatar(source)); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func roundAvatar(source image.Image) *image.NRGBA {
	avatar := image.NewNRGBA(image.Rect(0, 0, avatarSize, avatarSize))
	xdraw.CatmullRom.Scale(avatar, avatar.Bounds(), source, source.Bounds(), draw.Src, nil)
	radius := float64(avatarSize) / 2
	for y := range avatarSize {
		for x := range avatarSize {
			pixel := avatar.NRGBAAt(x, y)
			coverage := min(1, max(0, radius+0.5-math.Hypot(float64(x)+0.5-radius, float64(y)+0.5-radius)))
			pixel.A = uint8(float64(pixel.A) * coverage)
			avatar.SetNRGBA(x, y, pixel)
		}
	}
	return avatar
}

func writeContributorAvatars(contributors []contributor, avatars [][]byte) error {
	dir := filepath.Join(outDir, "contributors")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	wanted := make(map[string]bool, len(contributors))
	for i, contributor := range contributors {
		name := contributor.Login + ".png"
		wanted[name] = true
		if err := os.WriteFile(filepath.Join(dir, name), avatars[i], 0o644); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".png") && !wanted[entry.Name()] {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func fillContributors(contributors []contributor) error {
	var body strings.Builder
	body.WriteByte('\n')
	for i, contributor := range contributors {
		if i > 0 {
			body.WriteByte(' ')
		}
		fmt.Fprintf(
			&body,
			"<a href=\"%s\"><img src=\"docs/badges/contributors/%s.png\" width=\"64\" height=\"64\" alt=\"@%s\"></a>",
			html.EscapeString(contributor.HTMLURL),
			html.EscapeString(contributor.Login),
			html.EscapeString(contributor.Login),
		)
	}
	body.WriteByte('\n')
	_, err := fillREADMERegion("contributors", body.String())
	return err
}
