package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/mholt/caddy/caddyhttp/httpserver"
)

// RateLimit is an http.Handler that can limit request rate to specific paths or files
type RateLimit struct {
	Next  httpserver.Handler
	Rules []Rule
}

// Rule is a configuration for ratelimit
type Rule struct {
	Methods   string
	Rate      int64
	Burst     int
	Unit      string
	Whitelist []string
	Resources []string
}

const (
	ignoreSymbol = "^"
)

var (
	caddyLimiter    *CaddyLimiter
	whitelistIPNets []*net.IPNet
)

func init() {

	caddyLimiter = NewCaddyLimiter()
}

func (rl RateLimit) ServeHTTP(w http.ResponseWriter, r *http.Request) (int, error) {

	retryAfter := time.Duration(0)

	// TODO: move calculation block to pre setup(load config)

	// handle exception and get whitelist IPNet first
	for _, rule := range rl.Rules {
		for _, res := range rule.Resources {
			if strings.HasPrefix(res, ignoreSymbol) {
				res = strings.TrimPrefix(res, ignoreSymbol)
				if httpserver.Path(r.URL.Path).Matches(res) {
					return rl.Next.ServeHTTP(w, r)
				}
			}
		}
		for _, s := range rule.Whitelist {
			_, ipNet, err := net.ParseCIDR(s)
			if err == nil {
				whitelistIPNets = append(whitelistIPNets, ipNet)
			}
		}
	}

	for _, rule := range rl.Rules {
		for _, res := range rule.Resources {
			if !httpserver.Path(r.URL.Path).Matches(res) {
				continue
			}

			// filter whitelist ips
			address, err := GetRemoteIP(r)
			if err != nil {
				return http.StatusInternalServerError, err
			}
			// FIXME: whitelist shouldn't apply to all rules
			if IsWhitelistIPAddress(address, whitelistIPNets) || !MatchMethod(rule.Methods, r.Method) {
				continue
			}

			sliceKeys := buildKeys(rule.Methods, res, r)
			for _, keys := range sliceKeys {
				ret := caddyLimiter.Allow(keys, rule)
				if !ret {
					retryAfter = caddyLimiter.RetryAfter(keys)
					w.Header().Add("X-RateLimit-RetryAfter", retryAfter.String())
					return http.StatusTooManyRequests, nil
				}
			}
		}
	}

	return rl.Next.ServeHTTP(w, r)
}
