
> Open To-Do
> 
> In case you are the maintainer of a new SAP open source project, these are the steps to do with the template files:
> - Enter the correct metadata for the REUSE tool. See our [wiki page](https://wiki.wdf.sap.corp/wiki/display/ospodocs/Using+the+Reuse+Tool+of+FSFE+for+Copyright+and+License+Information) for details how to do it. You can find an initial open-component-mo file to build on. Please replace the parts inside the single angle quotation marks < > by the specific information for your repository and be sure to run the REUSE tool to validate that the metadata is correct.
> - Adjust the contribution guidelines (e.g. add coding style guidelines, pull request checklists, different license if needed etc.)
> - Add information about your project to this README (name, description, requirements etc). Especially take care for the <your-project> placeholders - those ones need to be replaced with your project name. See the sections below the horizontal line and [our guidelines on our wiki page](https://wiki.wdf.sap.corp/wiki/display/ospodocs/Guidelines+for+README.md+file) what is required and recommended.

# Open Component Model

[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/10065/badge)](https://www.bestpractices.dev/projects/10065)
[![REUSE status](https://api.reuse.software/badge/github.com/open-component-model/open-component-model)](https://api.reuse.software/info/github.com/open-component-model/open-component-model)

The Open Component Model (OCM) is an open standard to describe software bills of delivery (SBOD). OCM is a technology-agnostic and machine-readable format focused on the software artifacts that must be delivered for software products.

Check out the [the main OCM project web page](https://ocm.software) to find out what OCM offers you for implementing a secure software supply chain. It is your central entry point to all kind of OCM related [docs and guides](https://ocm.software/docs/overview/about), the [OCM specification](https://ocm.software/docs/overview/specification/) and all project [github repositories](https://github.com/open-component-model). It also offers a [Getting Started](https://ocm.software/docs/getting-started/) to quickly make your hands dirty with OCM, its toolset and concepts :smiley:


## OCM Specifications

OCM describes delivery [artifacts](https://github.com/open-component-model/ocm-spec/tree/main/doc/01-model/02-elements-toplevel.md#artifacts-resources-and-sources) that can be accessed from many types of [component repositories](https://github.com/open-component-model/ocm-spec/tree/main/doc/01-model/01-model.md#component-repositories). It defines a set of semantic, formatting, and other types of specifications that can be found in the [`ocm-spec` repository](https://github.com/open-component-model/ocm-spec). Start learning about the core concepts of OCM elements [here](https://github.com/open-component-model/ocm-spec/tree/main/doc/01-model/02-elements-toplevel.md#model-elements).


## OCM Library

This project provides a Go library containing an API for interacting with the
[Open Component Model (OCM)](https://github.com/open-component-model/ocm-spec) elements and mechanisms.

The library currently supports the following [repository mappings](https://github.com/open-component-model/ocm-spec/tree/main/doc/03-persistence/02-mappings.md#mappings-for-ocm-persistence):

- **OCI**: Use the repository prefix path of an OCI repository to implement an OCM
  repository.
- **CTF (Common Transport Format)**: Use a file-based binding to represent any set of
  component versions as filesystem content (directory, tar, tgz).

For the usage of the library to access OCM repositories, handle configuration and credentials see the [examples section](examples/lib/README.md).

Additionally, OCM provides a generic solution for how to:

- Sign component versions in any supported OCM repository implementation.
- Verify signatures based on public keys or verified certificates.
- Transport component versions, per reference or as values to any of the
  repository implementations.


## [OCM CLI](docs/reference/cli/ocm.md)

The [`ocm` CLI](docs/reference/cli/ocm.md) may also be used to interact with OCM mechanisms. It makes it easy to create component versions and embed them in build processes.

The code for the CLI can be found in [`cli`](cli).


## [OCM Language Bindings](bindings)

We supply language bindings for:

- [go](bindings/go)


## Licensing

Please see our [LICENSE](LICENSE) for copyright and license information.
Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/open-component-model/open-component-model).
