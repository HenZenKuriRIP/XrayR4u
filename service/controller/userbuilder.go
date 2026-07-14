package controller

import (
	"fmt"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/HenZenKuriRIP/XrayR4u/proxy/anytls"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/proxy/vless"
)

func (c *Controller) buildVlessUser(userInfo *[]api.UserInfo, enableVision bool) (users []*protocol.User) {
	users = make([]*protocol.User, len(*userInfo))
	for i, user := range *userInfo {
		vlessAccount := &vless.Account{
			Id: user.UUID,
		}
		if enableVision {
			vlessAccount.Flow = vless.XRV
		}
		users[i] = &protocol.User{
			Level:   0,
			Email:   c.buildUserTag(&user),
			Account: serial.ToTypedMessage(vlessAccount),
		}
	}
	return users
}

// buildAnyTLSUser converts panel UserInfo to anytls User.
// The User.Name is set to the composite key "tag|email|uid" so that the
// limiter (GetUserBucket / online device tracking) and traffic stats all
// work without any changes to the mydispatcher layer.
// The User.Password is the panel UUID, which sing-anytls internally SHA-256
// hashes for authentication lookup.
func (c *Controller) buildAnyTLSUser(userInfo *[]api.UserInfo) []anytls.User {
	users := make([]anytls.User, len(*userInfo))
	for i, user := range *userInfo {
		users[i] = anytls.User{
			Name:     c.buildUserTag(&user),
			Password: user.UUID,
		}
	}
	return users
}

func (c *Controller) buildUserTag(user *api.UserInfo) string {
	return fmt.Sprintf("%s|%s|%d", c.getState().tag, user.Email, user.UID)
}
