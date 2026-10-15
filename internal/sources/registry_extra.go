package sources

import "github.com/HVHBIGNAME/proxypulse/internal/proxy"

func additionalSources() []Source {
	const raw = "https://raw.githubusercontent.com/"
	feeds := []struct{ id, repo, path, kind string }{
		{"iplocate-http", "iplocate/free-proxy-list", "protocols/http.txt", proxy.KindHTTP},
		{"iplocate-https", "iplocate/free-proxy-list", "protocols/https.txt", proxy.KindHTTP},
		{"iplocate-socks5", "iplocate/free-proxy-list", "protocols/socks5.txt", proxy.KindSOCKS5},
		{"ercin-http", "ErcinDedeoglu/proxies", "proxies/http.txt", proxy.KindHTTP},
		{"ercin-https", "ErcinDedeoglu/proxies", "proxies/https.txt", proxy.KindHTTP},
		{"ercin-socks5", "ErcinDedeoglu/proxies", "proxies/socks5.txt", proxy.KindSOCKS5},
		{"zaeem-http", "Zaeem20/FREE_PROXIES_LIST", "http.txt", proxy.KindHTTP},
		{"zaeem-https", "Zaeem20/FREE_PROXIES_LIST", "https.txt", proxy.KindHTTP},
		{"zaeem-socks5", "Zaeem20/FREE_PROXIES_LIST", "socks5.txt", proxy.KindSOCKS5},
		{"mmpx-http", "mmpx12/proxy-list", "http.txt", proxy.KindHTTP},
		{"mmpx-https", "mmpx12/proxy-list", "https.txt", proxy.KindHTTP},
		{"mmpx-socks5", "mmpx12/proxy-list", "socks5.txt", proxy.KindSOCKS5},
		{"jetkai-http", "jetkai/proxy-list", "online-proxies/txt/proxies-http.txt", proxy.KindHTTP},
		{"jetkai-https", "jetkai/proxy-list", "online-proxies/txt/proxies-https.txt", proxy.KindHTTP},
		{"jetkai-socks5", "jetkai/proxy-list", "online-proxies/txt/proxies-socks5.txt", proxy.KindSOCKS5},
		{"kang-http", "officialputuid/KangProxy", "http/http.txt", proxy.KindHTTP},
		{"kang-https", "officialputuid/KangProxy", "https/https.txt", proxy.KindHTTP},
		{"kang-socks5", "officialputuid/KangProxy", "socks5/socks5.txt", proxy.KindSOCKS5},
		{"vpslab-http", "VPSLabCloud/VPSLab-Free-Proxy-List", "http_all.txt", proxy.KindHTTP},
		{"vpslab-socks5", "VPSLabCloud/VPSLab-Free-Proxy-List", "socks5_all.txt", proxy.KindSOCKS5},
		{"thordata-http", "Thordata/awesome-free-proxy-list", "proxies/http.txt", proxy.KindHTTP},
		{"thordata-https", "Thordata/awesome-free-proxy-list", "proxies/https.txt", proxy.KindHTTP},
		{"thordata-socks5", "Thordata/awesome-free-proxy-list", "proxies/socks5.txt", proxy.KindSOCKS5},
		{"proxyscrape-mirror-http", "ProxyScrape/free-proxy-list", "proxies/protocols/http/data.txt", proxy.KindHTTP},
		{"proxyscrape-mirror-https", "ProxyScrape/free-proxy-list", "proxies/protocols/https/data.txt", proxy.KindHTTP},
		{"proxyscrape-mirror-socks5", "ProxyScrape/free-proxy-list", "proxies/protocols/socks5/data.txt", proxy.KindSOCKS5},
		{"r00tee-https", "r00tee/Proxy-List", "Https.txt", proxy.KindHTTP},
		{"r00tee-socks5", "r00tee/Proxy-List", "Socks5.txt", proxy.KindSOCKS5},
		{"dpangestuw-http", "dpangestuw/Free-Proxy", "http_proxies.txt", proxy.KindHTTP},
		{"dpangestuw-socks5", "dpangestuw/Free-Proxy", "socks5_proxies.txt", proxy.KindSOCKS5},
	}
	out := make([]Source, 0, len(feeds))
	for _, feed := range feeds {
		out = append(out, Source{ID: feed.id, Name: feed.id, Kind: feed.kind, Format: "ipport", URL: raw + feed.repo + "/HEAD/" + feed.path})
	}
	return out
}
