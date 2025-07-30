# Porch Database Upgrades

This directory contains SQL scripts for upgrading the Porch database schema between versions.

## Files

- `upgrade_1.5.1_to_1.5.2.sql` - Upgrades from version 1.5.1 to 1.5.2

## Usage

1. **Sync** the database with the repositories
2. **Backup your database** before running any upgrade script
3. **Test on non-production environment** first
4. Run the appropriate upgrade script

#### Unix
```bash
psql -h 172.18.255.202 -p 55432 -U porch -d porch -f upgrade_1.5.1_to_1.5.2.sql
```

#### Windows
```bash
psql -h localhost -p 5432 -U porch -d porch -f upgrade_1.5.1_to_1.5.2.sql
```