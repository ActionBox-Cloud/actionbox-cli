import os
from pathlib import Path

from setuptools import setup

readme = Path(__file__).resolve().parent / "README.md"
long_description = readme.read_text() if readme.exists() else "Actionbox is a small, durable inbox where scripts and agents ask a human to make a decision, then continue through a signed callback."

setup(
    name="actionbox",
    version=os.environ["ACTIONBOX_VERSION"],
    description="input() for software that cannot wait at the terminal",
    long_description=long_description,
    long_description_content_type="text/markdown",
    license="MIT",
    # Older platform builders emit Metadata-Version 2.1. A License-File field
    # is valid only in 2.4+, so keep wheel metadata portable. The MIT license
    # remains explicit in metadata and in the repository LICENSE.
    license_files=[],
    url="https://actionbox.cloud",
    project_urls={
        "Source": "https://github.com/ActionBox-Cloud/actionbox-cli",
        "Issues": "https://github.com/ActionBox-Cloud/actionbox-cli/issues",
        "Documentation": "https://actionbox.cloud/docs",
    },
    classifiers=[
        "Development Status :: 4 - Beta",
        "Environment :: Console",
        "Intended Audience :: Developers",
        "License :: OSI Approved :: MIT License",
        "Operating System :: MacOS",
        "Operating System :: Microsoft :: Windows",
        "Operating System :: POSIX :: Linux",
        "Programming Language :: Python :: 3",
        "Topic :: Software Development :: Build Tools",
    ],
    python_requires=">=3.9",
    packages=["actionbox_pkg"],
    package_data={"actionbox_pkg": ["bin/*"]},
    entry_points={"console_scripts": ["actionbox = actionbox_pkg.launcher:main"]},
    options={
        "bdist_wheel": {
            "plat_name": os.environ["ACTIONBOX_WHEEL_PLAT"],
        }
    },
)
