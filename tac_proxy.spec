%global commit      f3dd2e9a642e3c74846b753fdaedbba3bee24396
%global shortcommit %(c=%{commit}; echo ${c:0:7})

Name:           tac-proxy
Version:        1.0.0
Release:        1
Summary:        This application will act as a proxy for tacacs between two servers
License:        MIT
URL:            http://github.com/mrevilme/tac_proxy
Source0:        https://github.com/example/app/archive/v%{version}.tar.gz
Source1:        tac-proxy.service
Source2:        tac-proxy.sysconfig

BuildRequires:  gcc

BuildRequires:  golang >= 1.2-7

%description
# include your full description of the application here.

%prep
%setup -q -n example-app-%{version}

# many golang binaries are "vendoring" (bundling) sources, so remove them. Those dependencies need to be packaged independently.
rm -rf vendor

%build
# set up temporary build gopath, and put our directory there
mkdir -p ./_build/src/github.com/example
ln -s $(pwd) ./_build/src/github.com/example/app

export GOPATH=$(pwd)/_build:%{gopath}
go build -o tac-proxy .

%install
install -d %{buildroot}%{_bindir}
install -p -m 0755 ./example-app %{buildroot}%{_bindir}/example-app

%files
%defattr(-,root,root,-)
%{_bindir}/tac-proxy

%changelog
* Tue Sep 06 2016 Emil Palm <emil@x86.nu> - 1.0.0
- package the tac-proxy
