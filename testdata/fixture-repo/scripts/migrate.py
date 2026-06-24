"""Database migration helpers for the fixture repository."""

from dataclasses import dataclass
from typing import Iterable, List


@dataclass
class Migration:
    version: int
    description: str
    sql: str


def load_migrations() -> List[Migration]:
    return [
        Migration(1, "create users table", "CREATE TABLE users (id TEXT PRIMARY KEY);"),
        Migration(2, "create payments table", "CREATE TABLE payments (id TEXT PRIMARY KEY);"),
    ]


def pending_migrations(applied: Iterable[int]) -> List[Migration]:
    applied_set = set(applied)
    return [m for m in load_migrations() if m.version not in applied_set]


def apply_migration(conn, migration: Migration) -> None:
    conn.execute(migration.sql)
    conn.execute("INSERT INTO schema_version (version) VALUES (?)", (migration.version,))


def run_migrations(conn, applied: Iterable[int]) -> int:
    count = 0
    for migration in pending_migrations(applied):
        apply_migration(conn, migration)
        count += 1
    return count
