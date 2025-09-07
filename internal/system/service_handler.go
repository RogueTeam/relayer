package system

type ServiceHandler interface {
	Restart(name string) (err error)
	Stop(name string) (err error)
	Enable(name string) (err error)
	Disable(name string) (err error)
	DaemonReload() (err error)
}
