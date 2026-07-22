<img src="ui/public/favicon.svg" alt="Portcullio logo" width="40" height="80" align="left">

# Portcullio

[![Latest release](https://img.shields.io/github/v/release/IvanBez42/Portcullio)](https://github.com/IvanBez42/Portcullio/releases)

Portcullio is a project to simplify the process of encrypting LUKS partitions for Docker Services. Lowers the barrier of entry for homelabs to have encrypt-at-rest fully automated by including a User Interface to easily manage and decrypt storage for services anywhere. Lockers can easily be linked to any docker services, allowing a single unlock to bring up any specified services that were fully encrypted.

## Using Prebuilt Images:

Instead of building from source, you can pull the published images directly:

```yaml
name: portcullio

services:
  agent:
    image: ghcr.io/ivanbez42/portcullio-agent:latest
    privileged: true
    volumes:
      - ./data/lockers:/lockers
      - ./data/mounts:/mounts:rshared
      - ./data/socket:/socket
      - /var/run/docker.sock:/var/run/docker.sock
      - /lib/modules:/lib/modules:ro
    restart: unless-stopped

  ui:
    image: ghcr.io/ivanbez42/portcullio-ui:latest
    ports:
      - "8080:8080"
    volumes:
      - ./data/socket:/socket:ro
      - ./data/ui-state:/state
    restart: unless-stopped
    depends_on:
      - agent
```

---

## Basic Docker Setup:

```yaml
name: portcullio

services:
  agent:
    build:
      context: ./agent
    privileged: true
    volumes:
      - ./data/lockers:/lockers
      - ./data/mounts:/mounts:rshared
      - ./data/socket:/socket
      - /var/run/docker.sock:/var/run/docker.sock
      - /lib/modules:/lib/modules:ro
    restart: unless-stopped

  ui:
    build:
      context: ./ui
    ports:
      - "8080:8080"
    volumes:
      - ./data/socket:/socket:ro
      - ./data/ui-state:/state
    restart: unless-stopped
    depends_on:
      - agent
```

## Privilege Disclosure:

Agent requires `/var/run/docker.sock` to start docker services upon unlocking and `privileged: true` unlock the LUKS encryptions. Both are isolated to the Agent container on purpose to make auditing of these permissions easier. It's encouraged to always audit code with these permissions.

## Environment Variables:

All environmental variables are optional and are set to reasonable and tested values by default. View all variables accepted below:

| Variable                                     | Default | Applies to | Description                                                                                                                                         |
| -------------------------------------------- | ------- | ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PORTCULLIO_FSTYPE`                          | `ext4`  | `agent`    | Filesystem used when formatting/mounting a locker.                                                                                                  |
| `PORTCULLIO_SEAL_HANDLE_TIMEOUT`             | `10s`   | `agent`    | How long an interactive seal (clicking "Lock" in the UI) waits for open file handles under the mount to clear before aborting.                      |
| `PORTCULLIO_SEAL_POLL_INTERVAL`              | `200ms` | `agent`    | Poll interval while waiting on the above.                                                                                                           |
| `PORTCULLIO_STARTUP_RECONCILE_TIMEOUT`       | `2s`    | `agent`    | Same idea, but for the one-time auto-heal check at agent startup -- deliberately shorter so a bad locker can't hang the whole daemon from starting. |
| `PORTCULLIO_STARTUP_RECONCILE_POLL_INTERVAL` | `100ms` | `agent`    | Poll interval for the startup check above.                                                                                                          |
| `PORT`                                       | `8080`  | `ui`       | Port the web UI listens on inside its container.                                                                                                    |

To set any of these, add an `environment:` block to the relevant service in the compose file above.

## Setup Advice:

_Full Disk encryption:_ Point the Lockers path to an external drive , and then in the interface create a locker the size of the full external device. All services can dynamically use space within this external storage.

_Per Service encryption:_ Create a locker for each service you want to encrypt, then allocate that service to it. Note this service will have a static size!

## License:

Portcullio is licensed under the [GNU General Public License v3.0](LICENSE).
