package handlers

import (
	"fmt"
	"strconv"
	"strings"
)

const noInstallationSessionInstallationID int64 = -1

func parseInstallationID(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("installation_id is empty")
	}

	installationID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || installationID <= 0 {
		return 0, fmt.Errorf("invalid installation_id: %s", value)
	}

	return installationID, nil
}

func oauthLoginRedirectURL(installationID int64) string {
	if installationID <= 0 {
		return "/auth/github/login"
	}
	return fmt.Sprintf("/auth/github/login?installation_id=%d", installationID)
}
