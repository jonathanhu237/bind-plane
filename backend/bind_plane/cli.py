import argparse
import asyncio

from sqlalchemy import select

from bind_plane.db.models import RoleName, User, UserRole
from bind_plane.db.session import async_session
from bind_plane.security.passwords import hash_password


async def create_admin(username: str, password: str, display_name: str | None) -> None:
    async with async_session() as session:
        existing = await session.scalar(select(User).where(User.username == username))
        if existing is not None:
            raise SystemExit(f"User already exists: {username}")

        user = User(
            username=username,
            display_name=display_name,
            password_hash=hash_password(password),
            must_change_password=True,
        )
        user.roles = [UserRole(role=RoleName.ADMIN)]
        session.add(user)
        await session.commit()


def main() -> None:
    parser = argparse.ArgumentParser(prog="bind-plane")
    subparsers = parser.add_subparsers(dest="command", required=True)

    create_admin_parser = subparsers.add_parser("create-admin")
    create_admin_parser.add_argument("username")
    create_admin_parser.add_argument("password")
    create_admin_parser.add_argument("--display-name")

    args = parser.parse_args()
    if args.command == "create-admin":
        asyncio.run(create_admin(args.username, args.password, args.display_name))


if __name__ == "__main__":
    main()
