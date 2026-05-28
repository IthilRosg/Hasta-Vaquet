package core

import "hasta-vaquet/protocol"

// Deprecated: используйте protocol.Config.
// LoadConfig загружает конфиг из JSON.
type Config = protocol.Config

var LoadConfig = protocol.LoadConfig
