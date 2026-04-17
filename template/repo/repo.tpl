package repository

import (
	"onecloud/ent/ent"
	"database/sql"

{{.EntPackage}}
)

type {{.Name}}Repository struct {
	client *ent.Client
	db     *sql.DB
}

func New{{.Name}}Repository(client *ent.Client) *{{.Name}}Repository {
	return &{{.Name}}Repository{client: client}
}