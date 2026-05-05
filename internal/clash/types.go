package clash

// Connection mirrors a single entry in mihomo's GET /connections response.
// Only fields tuntrace consumes are typed; mihomo emits more.
type Connection struct {
	ID       string   `json:"id"`
	Upload   int64    `json:"upload"`
	Download int64    `json:"download"`
	Start    string   `json:"start"`
	Chains   []string `json:"chains"`
	Rule     string   `json:"rule"`
	Metadata Metadata `json:"metadata"`
}

// Metadata reflects mihomo's connection metadata. Process / ProcessPath are
// only populated when the user has set `find-process-mode: always` (or strict)
// in their profile.yaml — see project docs for why.
type Metadata struct {
	Network         string `json:"network"`         // tcp | udp
	Type            string `json:"type"`            // HTTP | Socks | Tun | Redir | Mixed | ...
	SourceIP        string `json:"sourceIP"`
	DestinationIP   string `json:"destinationIP"`
	SourcePort      string `json:"sourcePort"`
	DestinationPort string `json:"destinationPort"`
	Host            string `json:"host"`
	DNSMode         string `json:"dnsMode"`
	Process         string `json:"process"`
	ProcessPath     string `json:"processPath"`
}

type ConnectionsResponse struct {
	Connections   []Connection `json:"connections"`
	UploadTotal   int64        `json:"uploadTotal"`
	DownloadTotal int64        `json:"downloadTotal"`
}
