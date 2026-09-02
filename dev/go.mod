module github.com/oneregex/revera/dev

go 1.27.0

require (
	github.com/oneregex/revera/go v0.0.0
	github.com/oneregex/revera/vego v0.0.0
)

replace (
	github.com/oneregex/revera/go => ../go
	github.com/oneregex/revera/vego => ../vego
)
