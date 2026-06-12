class SecCli < Formula
  desc "Extract structured data from SEC EDGAR filings for LLM consumption"
  homepage "https://github.com/kritidutta01/sec-cli"
  version "1.0.0"

  on_macos do
    on_arm do
      url "https://github.com/kritidutta01/sec-cli/releases/download/v#{version}/sec-cli_#{version}_darwin_arm64.tar.gz"
      sha256 "b04cb83244aab32625efa4a1599c614172283ad0817e3891bb190edc05fd7e33"
    end
    on_intel do
      url "https://github.com/kritidutta01/sec-cli/releases/download/v#{version}/sec-cli_#{version}_darwin_amd64.tar.gz"
      sha256 "c49be0b6fcce87f9225160b9a27e447fc3259f1c9dae55448030102d1831b5b6"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/kritidutta01/sec-cli/releases/download/v#{version}/sec-cli_#{version}_linux_arm64.tar.gz"
      sha256 "fd01c1eeaeb3f05eb516b45095dd6a127371b42f12da31f5265572e67fd41b61"
    end
    on_intel do
      url "https://github.com/kritidutta01/sec-cli/releases/download/v#{version}/sec-cli_#{version}_linux_amd64.tar.gz"
      sha256 "adaa40438c2649ab74eed050e7255b26600dc0d80fa3b4e22ad2d60e3ed6a3a3"
    end
  end

  def install
    bin.install "sec-cli"
  end

  test do
    # The binary exits 0 for version; anything else means the binary is broken.
    assert_match version.to_s, shell_output("#{bin}/sec-cli version")
  end
end
