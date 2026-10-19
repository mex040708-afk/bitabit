// Package library fija dónde vive cada versión dentro de biblioteca/. Lo usan
// la importación, que escribe ahí, y el endpoint /media, que lee: así no pueden
// discrepar.
package library

import "strconv"

// Dir es la carpeta, dentro de biblioteca/, de las versiones de una calidad:
// "aac" para aac256 y "orig" para el original.
func Dir(quality string) string {
	if quality == "aac256" {
		return "aac"
	}
	return "orig"
}

// File es la ruta de una versión dentro de biblioteca/, con '/' como separador
// (la forma que espera os.Root). Se deduce del ID: nunca viene del cliente.
func File(quality string, id int64, ext string) string {
	return Dir(quality) + "/" + strconv.FormatInt(id, 10) + "." + ext
}
