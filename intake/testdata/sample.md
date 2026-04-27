# Streambed

Streambed is a WAL-native CDC tool for Postgres. It pipes data into Iceberg/Parquet on S3 and queries it back over the Postgres wire protocol via DuckDB, with no external catalog dependency.

## Highlights

- Single Go binary, no JVM
- Reads directly from the Postgres WAL
- Stores in Iceberg/Parquet on S3
- Queries via DuckDB over the Postgres wire protocol
- No external catalog (Glue, Hive, Nessie) required

## Status

Pre-alpha. Currently dogfooding internally before public release.

Repo: https://github.com/example/streambed
