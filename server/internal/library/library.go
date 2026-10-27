// Package library fija dónde vive cada versión y cada carátula dentro de
// biblioteca/. Lo usan la importación, que escribe ahí, y el endpoint /media, que
// lee: así no pueden discrepar.
package library

import "strconv"

// CoverDir es la carpeta de las carátulas, dentro de biblioteca/.
const CoverDir = "covers"

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

// CoverFile es la ruta de la carátula de un álbum dentro de biblioteca/, con '/'
// como separador. Como las versiones, se deduce del ID del álbum.
func CoverFile(albumID int64, ext string) string {
	return CoverDir + "/" + strconv.FormatInt(albumID, 10) + "." + ext
}

// ThumbSize es el lado largo de la miniatura de una carátula (ADR-017).
const ThumbSize = 256

// ThumbFile es la ruta de la miniatura de la carátula de un álbum dentro de
// biblioteca/. Siempre es JPEG, sea cual sea el formato de la carátula.
func ThumbFile(albumID int64) string {
	return CoverDir + "/" + strconv.FormatInt(albumID, 10) + "-" + strconv.Itoa(ThumbSize) + ".jpg"
}
