package pkg

// Version 是当前应用版本号。需用 var（而非 const）才能被 ldflags 注入覆盖：
//
//	-ldflags "-X github.com/NeoPlayful/maple-gateway/server/pkg.Version=x.y.z"
var Version = "0.1.40"
