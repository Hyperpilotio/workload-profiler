package models

// CommandParameter descripes a command line tool's parameter
type CommandParameter struct {
	Arg         string `bson:"arg" json:"arg"`
	Description string `bson:"description" json:"description"`
}
