package web

import _ "embed"

//go:embed index.html
var Page []byte

//go:embed assist_macos.py
var MacPython []byte
