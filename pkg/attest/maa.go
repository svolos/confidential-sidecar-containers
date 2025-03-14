// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package attest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/microsoft/confidential-sidecars/pkg/common"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	AttestRequestURITemplate = "https://%s/attest/%s?%s"
)

// MAA contains information about the MAA service that acts as an authority
// for managed HSM service
type MAA struct {
	Endpoint   string `json:"endpoint"`
	TEEType    string `json:"tee_type,omitempty"`
	APIVersion string `json:"api_version,omitempty"`
}

// MAA SNP Request Body class
type maaReport struct {
	SNPReport string `json:"SnpReport"`
	CertChain string `json:"VcekCertChain"`
}

type attestedData struct {
	Data     string
	DataType string
}

type attestSNPRequestBody struct {
	Report       string       `json:"report"`
	RuntimeData  attestedData `json:"runtimeData"`
	InittimeData attestedData `json:"inittimeData"`
	Nonce        uint64       `json:"nonce"`
}

// newAttestSNPRequestBody constructs a MAA attest request. It contains (i) the base64
// URL encoding of a bundle containing the hardware attestation report (SNPReport)
// and the certificate chain, (ii) the  runtime data (base64 URL encoding of the public
// wrapping key), (iii) the inittime data (base64 URL encoding of the security policy),
// and (iv) a nonce
func newAttestSNPRequestBody(snpAttestationReport, vcekCertChain, policyBlob, keyBlob []byte) (*attestSNPRequestBody, error) {
	var request attestSNPRequestBody
	// the maa report is a bundle of the signed attestation report and
	// the cert chain that endorses the signing key
	maaReport := maaReport{
		SNPReport: base64.URLEncoding.EncodeToString(snpAttestationReport),
		CertChain: base64.URLEncoding.EncodeToString(vcekCertChain),
	}

	maaReportJSONBytes, err := json.Marshal(maaReport)
	if err != nil {
		return nil, errors.Wrapf(err, "marhalling maa Report field failed")
	}

	request.Report = base64.URLEncoding.EncodeToString(maaReportJSONBytes)

	// the key blob is passed as runtime data
	request.RuntimeData = attestedData{
		Data:     base64.URLEncoding.EncodeToString(keyBlob),
		DataType: "JSON",
	}

	// the policy blob is passed as inittime data
	if policyBlob != nil {
		request.InittimeData = attestedData{
			Data:     base64.URLEncoding.EncodeToString(policyBlob),
			DataType: "JSON",
		}
	}

	rand.Seed(time.Now().UnixNano())
	request.Nonce = rand.Uint64()

	return &request, nil
}

// attest interracts with MAA to fetch an MAA token. A valid MAA attest request requires a
// cert chain that endorses the signing key of the attestation report, the hardware attestation
// report, and additional evidence, including the policy blob and the key blob, whose hash have
// been included in the HOST_DATA and REPORT_DATA fields of the attestation report, respectively.
//
// MAA validates the signature of the attestation report using the public key of the leaf
// certificate of the cert chain, validates the cert chain, and finally validates the additional
// evidence against the HOST_DATA and REPORT_DATA fields of the validated attestation report.
// Upon successful attestation, MAA issues an MAA token which presents the policy blob as inittime
// claims and the key blob as runtime claims.
func (maa MAA) attest(SNPReportHexBytes []byte, vcekCertChain []byte, policyBlobBytes []byte, keyBlobBytes []byte) (MAAToken string, err error) {
	// Construct attestation request that contain the four attributes
	request, err := newAttestSNPRequestBody(SNPReportHexBytes, vcekCertChain, policyBlobBytes, keyBlobBytes)
	if err != nil {
		return "", errors.Wrapf(err, "creating new AttestSNPRequestBody failed")
	}

	maaRequestJSONData, err := json.Marshal(request)
	if err != nil {
		return "", errors.Wrapf(err, "marshalling maa request failed")
	}
	logrus.Debugf("MAA Request: %s\n", string(maaRequestJSONData))

	// HTTP POST request to MAA service
	uri := fmt.Sprintf(AttestRequestURITemplate, maa.Endpoint, maa.TEEType, maa.APIVersion)
	httpResponse, err := common.HTTPPRequest("POST", uri, maaRequestJSONData, "")
	if err != nil {
		return "", errors.Wrapf(err, "maa post request failed")
	}

	httpResponseBodyBytes, err := common.HTTPResponseBody(httpResponse)
	if err != nil {
		return "", errors.Wrapf(err, "pulling maa post response failed")
	}

	// Retrieve MAA token from the JWT response returned by MAA
	var maaResponse struct {
		Token string
	}

	if err = json.Unmarshal(httpResponseBodyBytes, &maaResponse); err != nil {
		return "", errors.Wrapf(err, "unmarshalling maa http response body failed")
	}

	if maaResponse.Token == "" {
		return "", errors.New("empty token string in maa response")
	}

	logrus.Debugf("MAA Token: %s", maaResponse.Token)
	return maaResponse.Token, nil
}
