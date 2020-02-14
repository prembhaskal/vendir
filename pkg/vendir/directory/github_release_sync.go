package directory

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"

	"github.com/cppforlife/go-cli-ui/ui"
)

type GithubReleaseSync struct {
	opts ConfigContentsGithubRelease
	ui   ui.UI
}

func (d GithubReleaseSync) Sync(dstPath string) (LockConfigContentsGithubRelease, error) {
	lockConf := LockConfigContentsGithubRelease{}
	incomingTmpPath := filepath.Join(incomingTmpDir, "github-release")

	err := os.MkdirAll(incomingTmpPath, 0700)
	if err != nil {
		return lockConf, fmt.Errorf("Creating incoming dir '%s' for git fetching: %s", incomingTmpPath, err)
	}

	defer os.RemoveAll(incomingTmpPath)

	releaseAPI, err := d.downloadRelease()
	if err != nil {
		return lockConf, fmt.Errorf("Downloading release info: %s", err)
	}

	lockConf.URL = releaseAPI.URL

	fileChecksums := map[string]string{}

	if !d.opts.DisableChecksumValidation {
		fileChecksums, err = ReleaseNotesChecksums{}.Find(releaseAPI.AssetNames(), releaseAPI.Body)
		if err != nil {
			return lockConf, fmt.Errorf("Finding checksums in release notes: %s", err)
		}
	}

	for _, asset := range releaseAPI.Assets {
		path := filepath.Join(incomingTmpPath, asset.Name)

		err := d.downloadFile(asset.BrowserDownloadURL, path)
		if err != nil {
			return lockConf, fmt.Errorf("Downloading asset '%s': %s", asset.Name, err)
		}

		err = d.checkFileSize(path, asset.Size)
		if err != nil {
			return lockConf, fmt.Errorf("Checking asset '%s' size: %s", asset.Name, err)
		}

		if !d.opts.DisableChecksumValidation {
			err = d.checkFileChecksum(path, fileChecksums[asset.Name])
			if err != nil {
				return lockConf, fmt.Errorf("Checking asset '%s' checksum: %s", asset.Name, err)
			}
		}
	}

	err = os.RemoveAll(dstPath)
	if err != nil {
		return lockConf, fmt.Errorf("Deleting dir %s: %s", dstPath, err)
	}

	err = os.Rename(incomingTmpPath, dstPath)
	if err != nil {
		return lockConf, fmt.Errorf("Moving directory '%s' to staging dir: %s", incomingTmpPath, err)
	}

	return lockConf, nil
}

func (d GithubReleaseSync) downloadRelease() (GithubReleaseAPI, error) {
	releaseAPI := GithubReleaseAPI{}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", d.opts.Slug, d.opts.Tag)

	resp, err := http.Get(url)
	if err != nil {
		return releaseAPI, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return releaseAPI, fmt.Errorf("Expected response status 200, but was '%d'", resp.StatusCode)
	}

	bs, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return releaseAPI, err
	}

	err = json.Unmarshal(bs, &releaseAPI)
	if err != nil {
		return releaseAPI, err
	}

	return releaseAPI, nil
}

func (d GithubReleaseSync) downloadFile(url string, dstPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func (d GithubReleaseSync) checkFileSize(path string, expectedSize int64) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.Size() != expectedSize {
		return fmt.Errorf("Expected file size to be %d, but was %d", expectedSize, fi.Size())
	}
	return nil
}

func (d GithubReleaseSync) checkFileChecksum(path string, expectedChecksum string) error {
	if len(expectedChecksum) == 0 {
		panic("Expected non-empty checksum as argument")
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	hash := sha256.New()
	_, err = io.Copy(hash, f)
	if err != nil {
		return fmt.Errorf("Calculating checksum: %s", err)
	}

	actualChecksum := fmt.Sprintf("%x", hash.Sum(nil))

	if actualChecksum != expectedChecksum {
		return fmt.Errorf("Expected file checksum to be '%s', but was '%s'",
			expectedChecksum, actualChecksum)
	}
	return nil
}

type GithubReleaseAPI struct {
	URL    string `json:"url"`
	Body   string
	Assets []GithubReleaseAssetAPI
}

type GithubReleaseAssetAPI struct {
	Name               string
	Size               int64
	BrowserDownloadURL string `json:"browser_download_url"`
}

func (a GithubReleaseAPI) AssetNames() []string {
	var result []string
	for _, asset := range a.Assets {
		result = append(result, asset.Name)
	}
	return result
}

/*

Example response (not all fields present):

{
  "url": "https://api.github.com/repos/jgm/pandoc/releases/22608933",
  "id": 22608933,
  "node_id": "MDc6UmVsZWFzZTIyNjA4OTMz",
  "tag_name": "2.9.1.1",
  "target_commitish": "master",
  "name": "pandoc 2.9.1.1",
  "draft": false,
  "assets": [
    {
      "url": "https://api.github.com/repos/jgm/pandoc/releases/assets/17158996",
      "id": 17158996,
      "node_id": "MDEyOlJlbGVhc2VBc3NldDE3MTU4OTk2",
      "name": "pandoc-2.9.1.1-windows-x86_64.zip",
      "label": null,
      "content_type": "application/zip",
      "state": "uploaded",
      "size": 36132549,
      "download_count": 9236,
      "created_at": "2020-01-06T05:16:32Z",
      "updated_at": "2020-01-06T05:23:48Z",
      "browser_download_url": "https://github.com/jgm/pandoc/releases/download/2.9.1.1/pandoc-2.9.1.1-windows-x86_64.zip"
    }
  ],
  "body": "..."
}

*/