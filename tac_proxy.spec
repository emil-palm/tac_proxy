%global provider        github
%global provider_tld    com
%global project         mrevilme
%global repo            tac_proxy
# https://github.com/mrevilme/tac_proxy
%global provider_prefix %{provider}.%{provider_tld}/%{project}/%{repo}
%global import_path     %{provider_prefix}
%define debug_package %{nil}

Name:           %{repo}
Version:        0
Release:        0.1
Summary:        A tacacs proxy to handle decoding and proxy of tacacs requests
License:        MIT
URL:            https://%{provider_prefix}
Source0:        https://%{provider_prefix}/archive/master.tar.gz

# e.g. el6 has ppc64 arch without gcc-go, so EA tag is required
ExclusiveArch:  %{?go_arches:%{go_arches}}%{!?go_arches:%{ix86} x86_64 %{arm}}
# If go_compiler is not set to 1, there is no virtual provide. Use golang instead.
BuildRequires:  %{?go_compiler:compiler(go-compiler)}%{!?go_compiler:golang}



%description
%{summary}

%prep
%setup -q -n %{repo}-master
%build
export GOPATH=$(pwd):%{gopath}
go build -o tac_proxy
%install
mkdir -p $RPM_BUILD_ROOT/{/etc/tac_proxy,/usr/bin,/etc/init.d}
install -m755 tac_proxy.init $RPM_BUILD_ROOT/etc/init.d/tac_proxy
install -m755 tac_proxy $RPM_BUILD_ROOT/usr/bin/tac_proxy
install -m644 resources/tac_proxy.yml $RPM_BUILD_ROOT/etc/tac_proxy/tac_proxy.yml
%check
%files
%{_bindir}/tac_proxy
%config(noreplace) /etc/tac_proxy/tac_proxy.yml
/etc/init.d/tac_proxy
%changelog
* Tue Sep 06 2016 root - 0-0.1.git0b0fb39
- First package for Fedora
