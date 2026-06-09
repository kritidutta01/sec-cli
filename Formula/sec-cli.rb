class SecCli < Formula
  desc "Extract structured data from SEC EDGAR filings for LLM consumption"
  homepage "https://github.com/kritidutta01/sec-cli"
  version "1.0.0"

  on_macos do
    on_arm do
      url "https://github.com/kritidutta01/sec-cli/releases/download/v#{version}/sec-cli_#{version}_darwin_arm64.tar.gz"
      sha256 "PLACEHOLDER_SHA256_DARWIN_ARM64"
    end
    on_intel do
      url "https://github.com/kritidutta01/sec-cli/releases/download/v#{version}/sec-cli_#{version}_darwin_amd64.tar.gz"
      sha256 "PLACEHOLDER_SHA256_DARWIN_AMD64"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/kritidutta01/sec-cli/releases/download/v#{version}/sec-cli_#{version}_linux_arm64.tar.gz"
      sha256 "PLACEHOLDER_SHA256_LINUX_ARM64"
    end
    on_intel do
      url "https://github.com/kritidutta01/sec-cli/releases/download/v#{version}/sec-cli_#{version}_linux_amd64.tar.gz"
      sha256 "PLACEHOLDER_SHA256_LINUX_AMD64"
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
