package web

import (
	"github.com/apeming/go-proxy-server/internal/auth"
	"github.com/apeming/go-proxy-server/internal/models"
)

func (wm *Manager) listUsers() ([]userResponse, error) {
	var users []models.User
	if err := wm.db.Order("username ASC").Find(&users).Error; err != nil {
		return nil, err
	}

	response := make([]userResponse, 0, len(users))
	for _, user := range users {
		response = append(response, userResponse{
			ID:        user.ID,
			IP:        user.IP,
			Username:  user.Username,
			CreatedAt: user.CreatedAt,
			UpdatedAt: user.UpdatedAt,
		})
	}

	return response, nil
}

func (wm *Manager) addUser(ip, username, password string) error {
	return auth.AddUser(wm.db, ip, username, password)
}

func (wm *Manager) deleteUser(username string) error {
	return auth.DeleteUser(wm.db, username)
}

func (wm *Manager) listWhitelist() []string {
	return auth.GetWhitelistIPs()
}

func (wm *Manager) addWhitelistIP(ip string) error {
	return auth.AddIPToWhitelist(wm.db, ip)
}

func (wm *Manager) deleteWhitelistIP(ip string) error {
	return auth.DeleteIPFromWhitelist(wm.db, ip)
}
