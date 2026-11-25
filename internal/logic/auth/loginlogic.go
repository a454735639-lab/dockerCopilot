package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt"
	"github.com/onlyLTY/dockerCopilot/internal/module"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type JwtResponse struct {
	Jwt string `json:"jwt"`
}

type LoginLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginLogic {
	return &LoginLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// Login 校验密钥并签发 JWT。
// clientIP 由 handler 传入，用于失败限速（同来源 10 分钟内 5 次失败锁定 10 分钟）。
func (l *LoginLogic) Login(req *types.LoginReq, clientIP string) (resp *types.Resp, err error) {
	resp = &types.Resp{}

	if remain, locked := module.AuthLocked(clientIP); locked {
		resp.Code = 429
		resp.Msg = fmt.Sprintf("尝试次数过多，请 %d 秒后再试", int(remain.Seconds())+1)
		resp.Data = map[string]string{"jwt": ""}
		return resp, nil
	}

	if l.svcCtx.Config.Auth.AccessSecret != req.SecretKey {
		if wait := module.RecordAuthFailure(clientIP); wait > 0 {
			l.Errorf("登录失败次数过多，来源 %s 已锁定", clientIP)
			resp.Code = 429
			resp.Msg = "失败次数过多，已锁定 10 分钟"
			resp.Data = map[string]string{"jwt": ""}
			return resp, nil
		}
		resp.Code = 401
		resp.Msg = "无效的secretKey"
		resp.Data = map[string]string{"jwt": ""}
		return resp, errors.New("无效的secretKey")
	}

	module.ResetAuthFailures(clientIP)

	jwtToken, err := l.getJwtToken(l.svcCtx.Config.Auth.AccessSecret,
		time.Now().Unix(),
		l.svcCtx.Config.Auth.AccessExpire,
	)
	if err != nil {
		resp.Code = 500
		resp.Msg = "无法生成 token，请重试"
		resp.Data = map[string]string{"jwt": ""}
		return resp, errors.New("生成 token出现错误，请重试")
	}
	resp.Code = 200
	resp.Msg = "success"
	resp.Data = JwtResponse{Jwt: jwtToken}
	return resp, nil
}

func (l *LoginLogic) getJwtToken(secretKey string, iat, seconds int64) (string, error) {
	claims := make(jwt.MapClaims)
	claims["iat"] = iat
	claims["exp"] = iat + seconds
	token := jwt.New(jwt.SigningMethodHS256)
	token.Claims = claims
	return token.SignedString([]byte(secretKey))
}
