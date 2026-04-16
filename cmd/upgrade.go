package cmd

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/jorgenbs/fido/internal/version"
	"github.com/spf13/cobra"
)

const repoAPI = "https://api.github.com/repos/jorgenbs/fido/releases/latest"

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade Fido to the latest release",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Checking for updates...")

		resp, err := http.Get(repoAPI)
		if err != nil {
			return fmt.Errorf("checking latest release: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return fmt.Errorf("GitHub API returned %d", resp.StatusCode)
		}

		var release ghRelease
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			return fmt.Errorf("parsing release: %w", err)
		}

		latest := strings.TrimPrefix(release.TagName, "v")
		current := strings.TrimPrefix(version.Version, "v")

		if current == latest {
			fmt.Printf("Already up to date (%s)\n", version.Version)
			return nil
		}

		wantSuffix := fmt.Sprintf("_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
		var downloadURL string
		for _, a := range release.Assets {
			if strings.HasSuffix(a.Name, wantSuffix) {
				downloadURL = a.BrowserDownloadURL
				break
			}
		}
		if downloadURL == "" {
			return fmt.Errorf("no release asset found for %s/%s", runtime.GOOS, runtime.GOARCH)
		}

		fmt.Printf("Downloading %s -> %s...\n", version.Version, release.TagName)

		dlResp, err := http.Get(downloadURL)
		if err != nil {
			return fmt.Errorf("downloading release: %w", err)
		}
		defer dlResp.Body.Close()

		tmpFile, err := extractBinaryFromTarGz(dlResp.Body)
		if err != nil {
			return fmt.Errorf("extracting binary: %w", err)
		}
		defer os.Remove(tmpFile)

		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("finding current executable: %w", err)
		}

		if err := replaceBinary(exe, tmpFile); err != nil {
			if os.IsPermission(err) {
				return fmt.Errorf("permission denied — try: sudo fido upgrade")
			}
			return fmt.Errorf("replacing binary: %w", err)
		}

		fmt.Printf("Upgraded: %s -> %s\n", version.Version, release.TagName)
		return nil
	},
}

func extractBinaryFromTarGz(r io.Reader) (string, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return "", err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if hdr.Typeflag == tar.TypeReg && (hdr.Name == "fido" || strings.HasSuffix(hdr.Name, "/fido")) {
			tmp, err := os.CreateTemp("", "fido-upgrade-*")
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(tmp, tr); err != nil {
				tmp.Close()
				os.Remove(tmp.Name())
				return "", err
			}
			tmp.Close()
			os.Chmod(tmp.Name(), 0755)
			return tmp.Name(), nil
		}
	}
	return "", fmt.Errorf("fido binary not found in archive")
}

func replaceBinary(dst, src string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	tmpPath := dst + ".new"
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, srcFile); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return err
	}
	out.Close()

	return os.Rename(tmpPath, dst)
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}
