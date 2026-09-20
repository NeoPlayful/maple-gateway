package gateway

// SetMaxInFlight 热更新数据面在途请求上限（0=不限）。供运行时配置变更时调用。
func (d *DataPlane) SetMaxInFlight(limit int) {
	if d.inflight != nil {
		d.inflight.SetLimit(limit)
	}
}

// SetMaxBodyBytes 热更新请求体大小上限（0=不限）。
func (d *DataPlane) SetMaxBodyBytes(limit int64) {
	if d.bodyLim != nil {
		d.bodyLim.SetLimit(limit)
	}
}
