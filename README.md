# Ubuntu Manpages Operator

**Ubuntu Manpages Operator** is a [charm](https://juju.is/charms-architecture) for deploying [https://manpages.ubuntu.com](https://manpages.ubuntu.com), a site which contains thousands of dynamically generated manuals, extracted from every support version of Ubuntu and updated on a regular basis.

This reposistory contains both the [application code](./app) and the code for the [charm](./src).

The application code was taken from the [original Launchpad repository](https://launchpad.net/ubuntu-manpage-repository), with some minor modifications to make it more easily configurable with the charm.

## Basic usage

Assuming you have access to a bootstrapped [Juju](https://juju.is) controller, you can deploy the charm with:

```bash
❯ juju deploy ubuntu-manpages
```

Once the charm is deployed, you can check the status with Juju status:

```bash
❯ juju status
Model    Controller           Cloud/Region         Version  SLA          Timestamp
testing  localhost-localhost  localhost/localhost  3.6.6    unsupported  11:06:36+01:00

App              Version  Status       Scale  Charm            Channel  Rev  Exposed  Message
ubuntu-manpages           maintenance      1  ubuntu-manpages             0  no       Updating manpages

Unit                Workload     Agent  Machine  Public address  Ports     Message
ubuntu-manpages/0*  maintenance  idle   1        10.245.163.53   8080/tcp  Updating manpages

Machine  State    Address        Inst id        Base          AZ  Message
1        started  10.245.163.53  juju-3a79fc-4  ubuntu@24.04      Running
```

You can see from the status that the application has been assigned an IP address, and is listening on port 8080. Using the example above, browsing to `http://10.245.163.53:8080` would display the homepage for the application.

On first start up, the charm will install the application, ensuring that any packages and configuration files are in place, and will begin downloading and processing manpages for the configured releases.

The charm accepts only one configuration option: `releases`, which is a comma-separated list of Ubuntu releases to include in the manpages, which you can adjust like so:

```bash
❯ juju config ubuntu-manpages releases="questing, plucky, oracular, noble"
```

When a new configuration is applied, the charm will automatically update the manpages to include the new releases, and purge any releases that are present on disk from a previous configuration, but no longer specified.

To update the manpages, you can use the provided Juju [Action](https://documentation.ubuntu.com/juju/3.6/howto/manage-actions/):

```bash
❯ juju run ubuntu-manpages/0 update-manpages
```

## Contribute to Ubuntu Manpages Operator

Snapcraft is open source and part of the Canonical family. We would love your help.

If you're interested, start with the [contribution guide](CONTRIBUTING.md).

## License and copyright

Ubuntu Manpages Operator is released under the [GPL-3.0 license](LICENSE).
