package models

// CommandParameter descripes a command line tool's parameter
type CommandParameter struct {
	Position    int    `bson:"pos" json:"pos"`
	Arg         string `bson:"arg" json:"arg"`
	Description string `bson:"description" json:"description"`
}
