//go:build !windows

// En plataformas distintas de Windows no hay DPAPI. Se devuelve el dato tal
// cual; la confidencialidad en reposo se apoya en permisos del fichero (0600) y
// ACL del directorio. En producción Linux/macOS, integrar Keychain / un
// almacén de claves del SO en este punto.
package config

func Protect(data []byte) ([]byte, error)   { return data, nil }
func Unprotect(data []byte) ([]byte, error) { return data, nil }
