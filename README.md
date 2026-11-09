# Fragrantica Scraper · Go

Ferramenta de linha de comando que coleta marcas e fragrâncias do Fragrantica em etapas e guarda os resultados e o progresso em MySQL.

## Rodar localmente

Use Go 1.22 ou posterior e um servidor MySQL. Crie um banco local, aplique [`schema.sql`](schema.sql) nele e preencha as variáveis `DB_*` em `.env`.

```bash
cp .env.example .env
go run . step1
go run . status
```

A primeira etapa coleta marcas por conexão direta. Para detalhar as marcas, listar as fragrâncias e depois coletar os detalhes de cada fragrância, configure o arquivo de proxies usando [`proxies.txt.example`](proxies.txt.example) e indique o caminho em `PROXY_FILE`.

```bash
go run . step2 --limit 5
go run . step3 --limit 10
go run . status
```

Remova `--limit` para processar todos os itens pendentes.
