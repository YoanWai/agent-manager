package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"time"

	xdraw "golang.org/x/image/draw"
)

const (
	avatarSize            = 64
	avatarGap             = 8
	maxContributorColumns = 12
)

type contributor struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
	Type      string `json:"type"`
}

// GitHub's contributors endpoint omits commit co-authors.
var additionalContributors = []contributor{
	{
		Login:     "reddeye1337",
		AvatarURL: "https://avatars.githubusercontent.com/u/261536237?v=4",
		Type:      "User",
	},
	{
		Login:     "mikaoelitiana",
		AvatarURL: "https://avatars.githubusercontent.com/u/674667?v=4",
		Type:      "User",
	},
	{
		Login:     "roshaans",
		AvatarURL: "https://avatars.githubusercontent.com/u/25015977?v=4",
		Type:      "User",
	},
}

func refreshContributors() error {
	var contributors []contributor
	if err := get("https://api.github.com/repos/"+repo+"/contributors?per_page=100", &contributors); err != nil {
		fmt.Printf("::warning::contributors were unavailable, so the published image was left alone: %v\n", err)
		return nil
	}
	contributors = addContributors(humanContributors(contributors), additionalContributors)
	avatars := make([]*image.NRGBA, len(contributors))
	for i, contributor := range contributors {
		avatar, err := circularAvatar(contributor.AvatarURL)
		if err != nil {
			fmt.Printf("::warning::the avatar for %s was unavailable, so the published image was left alone: %v\n", contributor.Login, err)
			return nil
		}
		avatars[i] = avatar
	}
	return writeContributorImage(avatars)
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

func circularAvatar(avatarURL string) (*image.NRGBA, error) {
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

	return roundAvatar(source), nil
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

func writeContributorImage(avatars []*image.NRGBA) error {
	if len(avatars) == 0 {
		return fmt.Errorf("no contributor avatars to render")
	}
	columns := min(len(avatars), maxContributorColumns)
	rows := (len(avatars) + columns - 1) / columns
	width := columns*avatarSize + (columns-1)*avatarGap
	height := rows*avatarSize + (rows-1)*avatarGap
	output := image.NewNRGBA(image.Rect(0, 0, width, height))
	for i, avatar := range avatars {
		x := (i % columns) * (avatarSize + avatarGap)
		y := (i / columns) * (avatarSize + avatarGap)
		draw.Draw(output, image.Rect(x, y, x+avatarSize, y+avatarSize), avatar, image.Point{}, draw.Over)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, output); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "contributors.png"), encoded.Bytes(), 0o644)
}
