// Package enums holds enumerations shared across layers.
package enums

type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvStaging     Environment = "staging"
	EnvProduction  Environment = "production"
	EnvTest        Environment = "test"
)

func Environments() []Environment {
	return []Environment{EnvDevelopment, EnvStaging, EnvProduction, EnvTest}
}

func (e Environment) IsValid() bool {
	for _, v := range Environments() {
		if e == v {
			return true
		}
	}
	return false
}

func (e Environment) String() string {
	return string(e)
}
