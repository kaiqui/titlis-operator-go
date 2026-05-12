package model

type SLOType string

const (
	SLOTypeMetric    SLOType = "metric"
	SLOTypeMonitor   SLOType = "monitor"
	SLOTypeTimeSlice SLOType = "time_slice"
)

type SLOTimeframe string

const (
	SLOTimeframe7d  SLOTimeframe = "7d"
	SLOTimeframe30d SLOTimeframe = "30d"
	SLOTimeframe90d SLOTimeframe = "90d"
)

type SLOAppFramework string

const (
	FrameworkWSGI    SLOAppFramework = "wsgi"
	FrameworkFastAPI SLOAppFramework = "fastapi"
	FrameworkAioHTTP SLOAppFramework = "aiohttp"
)

type SLO struct {
	ID               string
	Name             string
	ServiceName      string
	SLOType          SLOType
	TargetThreshold  float64
	WarningThreshold *float64
	Timeframe        SLOTimeframe
	Tags             []string
	Numerator        *string
	Denominator      *string
}

type ServiceDefinition struct {
	DDService   string
	Description *string
	Team        *string
	Tags        []string
}
