# git-remote-grpc

This project provides sample code for building a git remote helper that
allows for cloning, fetching, and pushing updates to remote git repositories
over gRPC.

This can be really useful for leveraging existing gRPC infrastructure,
such as TLS authentication support, to avoid having to build a separate
Public Key Infrastructure for authenticating with SSH keys.

To learn more, read the associated [blog post](http://encore.dev/blog/git-clone-grpc)
on the Encore Engineering Blog.